#!/usr/bin/env bash

set -euo pipefail

if [[ $# -lt 1 || $# -gt 2 ]]; then
  echo "usage: $0 <version> [output-directory]" >&2
  exit 2
fi

for command in go clang ldd realpath tar; do
  if ! command -v "$command" >/dev/null 2>&1; then
    echo "required command not found: $command" >&2
    exit 1
  fi
done

case $(uname -m) in
  x86_64) host_architecture=amd64 ;;
  aarch64|arm64) host_architecture=arm64 ;;
  *)
    echo "unsupported Linux release architecture: $(uname -m)" >&2
    exit 1
    ;;
esac
if [[ $(go env GOARCH) != "$host_architecture" ]]; then
  echo "release builds must use the native Go architecture: $host_architecture" >&2
  exit 1
fi

version=$1
output_directory=${2:-dist}
architecture=$host_architecture
archive_name="qk-${version}-linux-${architecture}"
staging_parent=$(mktemp -d)
staging_directory="${staging_parent}/${archive_name}"

cleanup() {
  rm -rf -- "$staging_parent"
}
trap cleanup EXIT

mkdir -p "$staging_directory/bin" "$staging_directory/lib" "$staging_directory/libs" "$output_directory"
cp -a libs/. "$staging_directory/libs/"

release_ldflags='-Wl,--disable-new-dtags,-rpath,$ORIGIN/../lib'
GOCACHE=${GOCACHE:-"${staging_parent}/go-build-cache"} \
  CGO_LDFLAGS="${CGO_LDFLAGS:-} ${release_ldflags}" \
  go build -trimpath -ldflags="-s -w -X=main.compilerVersion=${version}" -o "$staging_directory/bin/qkc" ./cmd/qkc

mapfile -t bundled_libraries < <(
  ldd "$staging_directory/bin/qkc" |
    awk '$2 == "=>" && $3 ~ /^\// { print $3 }' |
    grep -Ev '/(libc|libm|libdl|librt|libpthread)\.so(\.|$)|/ld-linux[^/]*\.so'
)

if ! printf '%s\n' "${bundled_libraries[@]}" | grep -E '/libLLVM' >/dev/null; then
  echo "no dynamic LLVM library was found in qkc" >&2
  exit 1
fi

for library in "${bundled_libraries[@]}"; do
  cp -L "$library" "$staging_directory/lib/$(basename "$library")"
done

bundle_library_directory=$(realpath "$staging_directory/lib")
mapfile -t resolved_bundled_libraries < <(
  ldd "$staging_directory/bin/qkc" |
    awk '$2 == "=>" && $3 ~ /^\// { print $3 }' |
    grep -Ev '/(libc|libm|libdl|librt|libpthread)\.so(\.|$)|/ld-linux[^/]*\.so'
)
for library in "${resolved_bundled_libraries[@]}"; do
  resolved_library=$(realpath "$library")
  if [[ "$resolved_library" != "$bundle_library_directory/"* ]]; then
    echo "qkc resolves a bundled dependency outside the release bundle: $library" >&2
    exit 1
  fi
done

reported_version=$("$staging_directory/bin/qkc" --version)
if [[ "$reported_version" != "qk compiler version ${version}" ]]; then
  echo "packaged qkc reported an unexpected version: $reported_version" >&2
  exit 1
fi
"$staging_directory/bin/qkc" -h >/dev/null
smoke_directory="$staging_parent/smoke"
mkdir -p "$smoke_directory"
printf 'module main\n\nlet main() { assert(true, "release libraries") }\n' >"$smoke_directory/main.qk"
"$staging_directory/bin/qkc" build -no-emit "$smoke_directory"

tar -C "$staging_parent" -czf "${output_directory}/${archive_name}.tar.gz" "$archive_name"
echo "created ${output_directory}/${archive_name}.tar.gz"
