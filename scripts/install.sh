#!/usr/bin/env bash

set -euo pipefail

if [[ $# -gt 1 ]]; then
  echo "usage: $0 [install-root]" >&2
  exit 2
fi

if [[ $# -eq 1 ]]; then
  install_root=$1
  bin_directory="$install_root/bin"
else
  configured_gobin=$(go env GOBIN)
  if [[ -n $configured_gobin ]]; then
    bin_directory=$configured_gobin
    install_root=$(dirname "$configured_gobin")
  else
    install_root=${GOPATH%%:*}
    if [[ -z $install_root ]]; then
      install_root=$(go env GOPATH)
      install_root=${install_root%%:*}
    fi
    bin_directory="$install_root/bin"
  fi
fi

mkdir -p "$bin_directory" "$install_root/libs"
GOBIN="$bin_directory" go install -trimpath ./cmd/qkc
cp -a libs/. "$install_root/libs/"

smoke_directory=$(mktemp -d)
cleanup() {
  rm -rf -- "$smoke_directory"
}
trap cleanup EXIT
printf 'module main\n\nlet main() { assert(true, "installed libraries") }\n' >"$smoke_directory/main.qk"
"$bin_directory/qkc" build -no-emit "$smoke_directory"

echo "installed qkc to $bin_directory/qkc"
echo "installed QK libraries to $install_root/libs"
