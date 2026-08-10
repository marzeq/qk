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

version=$1
output_directory=${2:-dist}
architecture=$(go env GOARCH)
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
  go build -trimpath -ldflags="-s -w" -o "$staging_directory/bin/qkc" ./cmd/qkc

mapfile -t bundled_libraries < <(
  ldd "$staging_directory/bin/qkc" |
    awk '$2 == "=>" && $3 ~ /^\// { print $3 }' |
    grep -Ev '/(libc|libm|libdl|librt|libpthread)\.so(\.|$)|/ld-linux[^/]*\.so'
)

if ! printf '%s\n' "${bundled_libraries[@]}" | grep -E '/lib(LLVM|clang|lld)' >/dev/null; then
  echo "no dynamic LLVM, Clang, or LLD libraries were found in qkc" >&2
  exit 1
fi

for library in "${bundled_libraries[@]}"; do
  cp -L "$library" "$staging_directory/lib/$(basename "$library")"
done

resource_directory=$(clang -print-resource-dir)
if [[ ! -d "$resource_directory" ]]; then
  echo "Clang resource directory not found: $resource_directory" >&2
  exit 1
fi
resource_version=$(basename "$resource_directory")
mkdir -p "$staging_directory/lib/clang"
cp -a "$resource_directory" "$staging_directory/lib/clang/$resource_version"

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

"$staging_directory/bin/qkc" -h >/dev/null
smoke_directory="$staging_parent/smoke"
mkdir -p "$smoke_directory"
printf 'module main\n\nlet main() { assert(true, "release libraries") }\n' >"$smoke_directory/main.qk"
"$staging_directory/bin/qkc" build -no-emit "$smoke_directory"

tar -C "$staging_parent" -czf "${output_directory}/${archive_name}.tar.gz" "$archive_name"
echo "created ${output_directory}/${archive_name}.tar.gz"
