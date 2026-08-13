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

for command in go otool install_name_tool codesign tar; do
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
clang_command=${CLANG:-"$llvm_prefix/bin/clang"}
if [[ ! -x "$clang_command" ]]; then
  echo "Clang not found: $clang_command" >&2
  exit 1
fi

if [[ -n ${LLD_ROOT:-} ]]; then
  lld_prefix=$LLD_ROOT
elif [[ -f "$llvm_library_directory/liblldCommon.dylib" ]]; then
  lld_prefix=$llvm_prefix
elif command -v brew >/dev/null 2>&1 && [[ -f $(brew --prefix lld)/lib/liblldCommon.dylib ]]; then
  lld_prefix=$(brew --prefix lld)
else
  echo "LLD development libraries not found; install LLD or set LLD_ROOT" >&2
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

mkdir -p "$staging_directory/bin" "$staging_directory/lib" "$staging_directory/libs" "$output_directory"
cp -a libs/. "$staging_directory/libs/"

release_ldflags='-Wl,-rpath,@executable_path/../lib'
GOCACHE=${GOCACHE:-"${staging_parent}/go-build-cache"} \
  CGO_CXXFLAGS="${CGO_CXXFLAGS:-} -I${llvm_prefix}/include -I${lld_prefix}/include" \
  CGO_LDFLAGS="${CGO_LDFLAGS:-} -L${llvm_library_directory} -L${lld_prefix}/lib ${release_ldflags}" \
  go build -trimpath -ldflags="-s -w -X=main.compilerVersion=${version}" -o "$staging_directory/bin/qkc" ./cmd/qkc

dependencies() {
  otool -L "$1" | awk 'NR > 1 { print $1 }'
}

is_system_library() {
  case "$1" in
    /usr/lib/*|/System/Library/*) return 0 ;;
    *) return 1 ;;
  esac
}

resolve_dependency() {
  dependency=$1
  owner=$2
  owner_directory=$(dirname "$owner")

  case "$dependency" in
    /*)
      [[ -f "$dependency" ]] && printf '%s\n' "$dependency" && return 0
      ;;
    @loader_path/*)
      candidate="${owner_directory}/${dependency#@loader_path/}"
      [[ -f "$candidate" ]] && printf '%s\n' "$candidate" && return 0
      ;;
    @executable_path/*)
      candidate="${staging_directory}/bin/${dependency#@executable_path/}"
      [[ -f "$candidate" ]] && printf '%s\n' "$candidate" && return 0
      ;;
    @rpath/*)
      name=${dependency#@rpath/}
      for directory in "$owner_directory" "$llvm_library_directory"; do
        [[ -f "$directory/$name" ]] && printf '%s\n' "$directory/$name" && return 0
      done
      ;;
  esac
  return 1
}

contains_path() {
  wanted=$1
  shift
  for path in "$@"; do
    [[ "$path" == "$wanted" ]] && return 0
  done
  return 1
}

queue=("$staging_directory/bin/qkc")
libraries=()
queue_index=0
while (( queue_index < ${#queue[@]} )); do
  owner=${queue[$queue_index]}
  queue_index=$((queue_index + 1))
  while IFS= read -r dependency; do
    is_system_library "$dependency" && continue
    if ! resolved=$(resolve_dependency "$dependency" "$owner"); then
      echo "could not resolve dependency $dependency required by $owner" >&2
      exit 1
    fi
    contains_path "$resolved" "${libraries[@]:-}" && continue
    libraries+=("$resolved")
    queue+=("$resolved")
  done < <(dependencies "$owner")
done

if ! printf '%s\n' "${libraries[@]}" | grep -E '/lib(LLVM|clang|lld)' >/dev/null; then
  echo "no dynamic LLVM, Clang, or LLD libraries were found in qkc" >&2
  exit 1
fi

for library in "${libraries[@]}"; do
  destination="$staging_directory/lib/$(basename "$library")"
  if [[ -e "$destination" ]]; then
    echo "two bundled libraries have the same filename: $(basename "$library")" >&2
    exit 1
  fi
  cp -L "$library" "$destination"
done

rewrite_dependencies() {
  target=$1
  while IFS= read -r dependency; do
    is_system_library "$dependency" && continue
    name=$(basename "$dependency")
    if [[ -f "$staging_directory/lib/$name" ]]; then
      install_name_tool -change "$dependency" "@rpath/$name" "$target"
    fi
  done < <(dependencies "$target")
}

rewrite_dependencies "$staging_directory/bin/qkc"
for library in "$staging_directory"/lib/*.dylib; do
  install_name_tool -id "@rpath/$(basename "$library")" "$library"
  rewrite_dependencies "$library"
  codesign --force --sign - "$library" >/dev/null
done
codesign --force --sign - "$staging_directory/bin/qkc" >/dev/null

resource_directory=$($clang_command -print-resource-dir)
if [[ ! -d "$resource_directory" ]]; then
  echo "Clang resource directory not found: $resource_directory" >&2
  exit 1
fi
resource_version=$(basename "$resource_directory")
mkdir -p "$staging_directory/lib/clang"
cp -a "$resource_directory" "$staging_directory/lib/clang/$resource_version"

for target in "$staging_directory/bin/qkc" "$staging_directory"/lib/*.dylib; do
  while IFS= read -r dependency; do
    case "$dependency" in
      /usr/lib/*|/System/Library/*|@rpath/*|@executable_path/*|@loader_path/*) ;;
      *)
        echo "$target retains a non-relocatable dependency: $dependency" >&2
        exit 1
        ;;
    esac
  done < <(dependencies "$target")
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
