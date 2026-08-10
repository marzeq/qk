#!/usr/bin/env bash

set -euo pipefail

if [[ $# -ne 0 ]]; then
  echo "usage: $0" >&2
  exit 2
fi

install_root=${QK_DEV_INSTALL_ROOT:-"$HOME/.local/share/qk"}
command_bin_directory=${QK_DEV_BIN_DIR:-"$HOME/.local/bin"}
bin_directory="$install_root/bin"

mkdir -p "$bin_directory" "$install_root/libs" "$command_bin_directory"
GOBIN="$bin_directory" go install -trimpath ./cmd/qkc
cp -a libs/. "$install_root/libs/"

command_link="$command_bin_directory/qkc"
if [[ -e $command_link && ! -L $command_link ]]; then
  rm -f -- "$command_link"
fi
ln -sfn "$bin_directory/qkc" "$command_link"

smoke_directory=$(mktemp -d)
cleanup() {
  rm -rf -- "$smoke_directory"
}
trap cleanup EXIT
printf 'module main\n\nlet main() { assert(true, "installed libraries") }\n' >"$smoke_directory/main.qk"
"$command_link" build -no-emit "$smoke_directory"

echo "installed qkc to $bin_directory/qkc"
echo "installed QK libraries to $install_root/libs"
echo "linked $command_link -> $bin_directory/qkc"
