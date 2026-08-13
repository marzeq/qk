#!/usr/bin/env bash

set -euo pipefail

if [[ $# -lt 1 || $# -gt 2 ]]; then
  echo "usage: $0 <version> [output-directory]" >&2
  exit 2
fi

for command in go clang readelf tar; do
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

if [[ -n ${LLVM_CONFIG:-} ]]; then
  llvm_config=$LLVM_CONFIG
elif command -v llvm-config-22 >/dev/null 2>&1; then
  llvm_config=$(command -v llvm-config-22)
elif command -v llvm-config >/dev/null 2>&1; then
  llvm_config=$(command -v llvm-config)
else
  echo "llvm-config not found; install static LLVM 22 development files or set LLVM_CONFIG" >&2
  exit 1
fi
if [[ $($llvm_config --version) != 22.* ]]; then
  echo "release builds require LLVM 22; $llvm_config reports $($llvm_config --version)" >&2
  exit 1
fi
llvm_prefix=$($llvm_config --prefix)
llvm_library_directory=$($llvm_config --libdir)
if ! static_llvm_libraries=$($llvm_config --link-static --libs all-targets passes irreader 2>/dev/null); then
  echo "LLVM static component archives are unavailable; install or build static LLVM 22" >&2
  exit 1
fi
if ! static_llvm_system_libraries=$($llvm_config --link-static --system-libs all-targets passes irreader 2>/dev/null); then
  echo "could not determine LLVM's static system-library dependencies" >&2
  exit 1
fi
# Debian's LLVM configuration records its optional Z3 dependency as an
# absolute shared-library path. QK does not use LLVM's Z3-backed APIs, and the
# corresponding object is not pulled from the static LLVM archives, so omit it
# from a fully static release link.
filtered_llvm_system_libraries=
for library in $static_llvm_system_libraries; do
  case $library in
    -lz3|*libz3.so|*libz3.so.*) ;;
    *) filtered_llvm_system_libraries+=" ${library}" ;;
  esac
done

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

mkdir -p "$staging_directory/bin" "$staging_directory/libs" "$output_directory"
cp -a libs/. "$staging_directory/libs/"
cp LICENSE "$staging_directory/LICENSE"

final_linker_flags="${CGO_LDFLAGS:-} -L${llvm_library_directory} ${static_llvm_libraries} ${filtered_llvm_system_libraries} -static"
GOCACHE=${GOCACHE:-"${staging_parent}/go-build-cache"} \
  CGO_CXXFLAGS="${CGO_CXXFLAGS:-} -I${llvm_prefix}/include" \
  CGO_LDFLAGS= \
  go build -tags qk_static_llvm -trimpath \
    -ldflags="-s -w -linkmode=external -extldflags '${final_linker_flags}' -X=main.compilerVersion=${version}" \
    -o "$staging_directory/bin/qkc" ./cmd/qkc

if readelf -l "$staging_directory/bin/qkc" 2>/dev/null | grep -q 'INTERP' ||
   readelf -d "$staging_directory/bin/qkc" 2>/dev/null | grep -q '(NEEDED)'; then
  echo "release qkc retains dynamic ELF dependencies" >&2
  readelf -d "$staging_directory/bin/qkc" >&2
  exit 1
fi

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
