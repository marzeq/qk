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

mkdir -p "$staging_directory/bin" "$staging_directory/lib" "$staging_directory/libs" "$output_directory"
cp -a libs/. "$staging_directory/libs/"

GOCACHE=${GOCACHE:-"${staging_parent}/go-build-cache"} \
  go build -trimpath -ldflags="-s -w -X=main.compilerVersion=${version}" \
  -o "$staging_directory/bin/qkc.exe" ./cmd/qkc

# Windows searches beside the executable for DLLs. ldd reports the complete
# UCRT64 dependency closure, so no MSYS2 installation is needed at runtime.
while IFS= read -r dependency; do
  case "$dependency" in
    /ucrt64/bin/*.dll) cp -L "$dependency" "$staging_directory/bin/" ;;
  esac
done < <(ldd "$staging_directory/bin/qkc.exe" | awk '{ print $3 }')

# qkc uses Clang's driver layout to locate the MinGW import libraries.
cp -L "$(command -v clang)" "$staging_directory/bin/clang.exe"
while IFS= read -r dependency; do
  case "$dependency" in
    /ucrt64/bin/*.dll) cp -L "$dependency" "$staging_directory/bin/" ;;
  esac
done < <(ldd "$staging_directory/bin/clang.exe" | awk '{ print $3 }')
# Copy the UCRT/MinGW startup objects and import libraries, but not the LLVM,
# Clang, or LLD development archives: qkc already carries their runtime DLLs.
while IFS= read -r library; do
  name=$(basename "$library")
  case "$name" in
    libLLVM*|libclang*|liblld*) continue ;;
  esac
  cp -L "$library" "$staging_directory/lib/"
done < <(find /ucrt64/lib -maxdepth 1 -type f \( -name '*.a' -o -name '*.o' \) -print)

resource_directory=$(clang -print-resource-dir)
if [[ ! -d "$resource_directory" ]]; then
  echo "Clang resource directory not found: $resource_directory" >&2
  exit 1
fi
resource_version=$(basename "$resource_directory")
mkdir -p "$staging_directory/lib/clang"
cp -a "$resource_directory" "$staging_directory/lib/clang/$resource_version"

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
