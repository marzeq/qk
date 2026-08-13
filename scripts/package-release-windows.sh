#!/usr/bin/env bash

set -euo pipefail

if [[ ${MSYSTEM:-} != UCRT64 ]]; then
  echo "this script must run in an MSYS2 UCRT64 shell" >&2
  exit 1
fi
if [[ $# -lt 1 || $# -gt 2 ]]; then
  echo "usage: $0 <version> [output-directory]" >&2
  exit 2
fi

for command in go clang ldd tar; do
  if ! command -v "$command" >/dev/null 2>&1; then
    echo "required command not found: $command" >&2
    exit 1
  fi
done
if [[ $(go env GOARCH) != amd64 ]]; then
  echo "Windows release packaging currently supports amd64 only" >&2
  exit 1
fi

if [[ -n ${LLVM_CONFIG:-} ]]; then
  llvm_config=$LLVM_CONFIG
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
static_zstd_library="${llvm_prefix}/lib/libzstd.a"
if [[ ! -f $static_zstd_library ]]; then
  echo "static zstd archive not found: $static_zstd_library" >&2
  exit 1
fi
filtered_llvm_system_libraries=
for library in $static_llvm_system_libraries; do
  case $library in
    -lz3|*libz3.dll.a) ;;
    -lzstd|*libzstd.dll.a) filtered_llvm_system_libraries+=" ${static_zstd_library}" ;;
    *) filtered_llvm_system_libraries+=" ${library}" ;;
  esac
done

version=$1
output_directory=${2:-dist}
architecture=amd64
archive_name="qk-${version}-windows-${architecture}"
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
    -o "$staging_directory/bin/qkc.exe" ./cmd/qkc

if ldd "$staging_directory/bin/qkc.exe" | grep -Eiq '(/ucrt64/|\\ucrt64\\|/mingw64/|\\mingw64\\)'; then
  echo "release qkc retains MSYS2/MinGW runtime DLL dependencies" >&2
  ldd "$staging_directory/bin/qkc.exe" >&2
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

reported_version=$(PATH="$staging_directory/bin:/usr/bin" "$staging_directory/bin/qkc.exe" --version)
if [[ "$reported_version" != "qk compiler version ${version}" ]]; then
  echo "packaged qkc reported an unexpected version: $reported_version" >&2
  exit 1
fi

smoke_directory="$staging_parent/smoke"
mkdir -p "$smoke_directory"
printf 'module main\n\nlet main() { assert(true, "release libraries") }\n' >"$smoke_directory/main.qk"
PATH="$staging_directory/bin:/usr/bin" "$staging_directory/bin/qkc.exe" build -no-emit "$smoke_directory"

tar -C "$staging_parent" -czf "${output_directory}/${archive_name}.tar.gz" "$archive_name"
echo "created ${output_directory}/${archive_name}.tar.gz"
