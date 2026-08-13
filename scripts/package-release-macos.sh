#!/usr/bin/env bash

set -euo pipefail

if [[ $(uname -s) != Darwin ]]; then
  echo "this script must run on macOS" >&2
  exit 1
fi
if [[ $# -lt 1 || $# -gt 2 ]]; then
  echo "usage: $0 <version> [output-directory]" >&2
  exit 2
fi

for command in go otool codesign tar; do
  if ! command -v "$command" >/dev/null 2>&1; then
    echo "required command not found: $command" >&2
    exit 1
  fi
done

case $(uname -m) in
  x86_64) host_architecture=amd64 ;;
  arm64) host_architecture=arm64 ;;
  *)
    echo "unsupported macOS release architecture: $(uname -m)" >&2
    exit 1
    ;;
esac
if [[ $(go env GOARCH) != "$host_architecture" ]]; then
  echo "release builds must use the native Go architecture: $host_architecture" >&2
  exit 1
fi

if [[ -n ${LLVM_CONFIG:-} ]]; then
  llvm_config=$LLVM_CONFIG
elif command -v llvm-config >/dev/null 2>&1; then
  llvm_config=$(command -v llvm-config)
elif command -v brew >/dev/null 2>&1 && [[ -x $(brew --prefix llvm)/bin/llvm-config ]]; then
  llvm_config=$(brew --prefix llvm)/bin/llvm-config
else
  echo "llvm-config not found; install LLVM or set LLVM_CONFIG" >&2
  exit 1
fi

llvm_prefix=$($llvm_config --prefix)
llvm_library_directory=$($llvm_config --libdir)
if [[ $($llvm_config --version) != 22.* ]]; then
  echo "release builds require LLVM 22; $llvm_config reports $($llvm_config --version)" >&2
  exit 1
fi
if ! static_llvm_libraries=$($llvm_config --link-static --libs all-targets passes irreader 2>/dev/null); then
  echo "LLVM static component archives are unavailable; install or build static LLVM 22" >&2
  exit 1
fi
if ! static_llvm_system_libraries=$($llvm_config --link-static --system-libs all-targets passes irreader 2>/dev/null); then
  echo "could not determine LLVM's static system-library dependencies" >&2
  exit 1
fi
version=$1
output_directory=${2:-dist}
architecture=$host_architecture
archive_name="qk-${version}-macos-${architecture}"
staging_parent=$(mktemp -d)
staging_directory="${staging_parent}/${archive_name}"

cleanup() {
  rm -rf -- "$staging_parent"
}
trap cleanup EXIT

mkdir -p "$staging_directory/bin" "$staging_directory/libs" "$output_directory"
cp -a libs/. "$staging_directory/libs/"
cp LICENSE "$staging_directory/LICENSE"

GOCACHE=${GOCACHE:-"${staging_parent}/go-build-cache"} \
  CGO_CXXFLAGS="${CGO_CXXFLAGS:-} -I${llvm_prefix}/include" \
  CGO_LDFLAGS="${CGO_LDFLAGS:-} -L${llvm_library_directory} ${static_llvm_libraries} ${static_llvm_system_libraries}" \
  go build -tags qk_static_llvm -trimpath \
    -ldflags="-s -w -linkmode=external -X=main.compilerVersion=${version}" \
    -o "$staging_directory/bin/qkc" ./cmd/qkc

dependencies() {
  otool -L "$1" | awk 'NR > 1 { print $1 }'
}

while IFS= read -r dependency; do
  case "$dependency" in
    /usr/lib/*|/System/Library/*) ;;
    *)
      echo "release qkc retains non-system dependency: $dependency" >&2
      exit 1
      ;;
  esac
done < <(dependencies "$staging_directory/bin/qkc")

llvm_license=
for candidate in \
  "$llvm_prefix/LICENSE.TXT" \
  "$llvm_prefix/share/llvm/LICENSE.TXT" \
  "$llvm_prefix/share/licenses/llvm/LICENSE" \
  "$llvm_prefix/share/licenses/llvm/LICENSE.TXT" \
  "$llvm_prefix/share/doc/llvm/LICENSE.TXT"; do
  if [[ -f $candidate ]]; then
    llvm_license=$candidate
    break
  fi
done
if [[ -z $llvm_license ]]; then
  echo "LLVM license file not found beneath $llvm_prefix" >&2
  exit 1
fi
cp "$llvm_license" "$staging_directory/LLVM-LICENSE.txt"
codesign --force --sign - "$staging_directory/bin/qkc" >/dev/null

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
