# qk

This is my work in progress compiler for a custom language I'm designing.

## Building

```bash
git clone git@github.com:marzeq/qk.git
cd qk
git config core.hooksPath .githooks # if you plan to contribute

go build ./cmd/qkc   # or run 'go run ./cmd/qkc' directly
```

For a relocatable installation containing both the compiler and its QK
libraries, use:

```bash
scripts/dev_install.sh
```

This installs the development release under `~/.local/share/qk/{bin,libs}` and
creates `~/.local/bin/qkc` as a symlink to the installed compiler. Plain
`go install ./cmd/qkc` installs only the executable because the Go tool has no
mechanism for installing repository data files.

## Testing

`go test ./...` runs all unit tests, including behavioural tests and a standard library type-check.

This is included as a pre-push git hook, and it will block pushes if any tests fail. You can bypass this by using `git push --no-verify`.

## Supported platforms

### For the compiler itself

Linux, macOS, and Windows hosts are supported when the required LLVM 22,
Clang C++, and LLD development libraries are available. Linux is currently the
most extensively exercised host.

### For compiling code with the compiler

The compiler supports recognised 32-bit and 64-bit target architectures. C ABI
aggregate lowering is currently limited to the documented target families.

## Dependencies

### Running a bundled release

Should be self contined within the extracted archive.

### Building the compiler yourself

All source builds require Go 1.23.5 or newer, cgo, a C++17 compiler, and a
matching LLVM 22 development installation containing:

- LLVM and Clang C++ headers, including `llvm/`, `clang/`, and `lld/`;
- the LLVM and Clang C++ libraries; and
- the LLD ELF, COFF, MinGW, Mach-O, WebAssembly, and Common driver libraries.

LLVM's command-line tools alone are insufficient. The headers and libraries
must match the selected C++ ABI and be visible through `CGO_CXXFLAGS` and
`CGO_LDFLAGS`.

#### Linux

Install Go, Clang/Clang++, and the LLVM 22, Clang C++, and LLD 22 development
packages supplied by your distribution or the LLVM project. Package names vary;
the installation must provide `llvm-config-22` and the libraries named in
`codegen/llvmbackend/link_dynamic.go`. Then run:

```bash
llvm_prefix=$(llvm-config-22 --prefix)
export CC=clang-22
export CXX=clang++-22
export CGO_ENABLED=1
export CGO_CXXFLAGS="-I${llvm_prefix}/include"
export CGO_LDFLAGS="-L${llvm_prefix}/lib"
go build ./cmd/qkc
go test ./...
```

Keep the same environment variables set for `go test`. If your distribution
installs LLD in a separate prefix, add its `include` and `lib` directories to
the corresponding cgo variables.

#### macOS

Install the Xcode Command Line Tools, Go, LLVM 22, and LLD 22. With Homebrew:

```bash
xcode-select --install
brew install go llvm lld
source ./scripts/macos-brew-init.sh
go build ./cmd/qkc
go test ./...
```

The initialization script locates the Homebrew prefixes and exports the cgo
include and library paths. LLD is a separate Homebrew formula.

#### Windows (MSYS2 UCRT64)

Run these commands in an MSYS2 UCRT64 shell. Keep all components in the same
UCRT64 ABI environment:

```bash
pacman -S --needed \
  mingw-w64-ucrt-x86_64-go \
  mingw-w64-ucrt-x86_64-clang \
  mingw-w64-ucrt-x86_64-llvm \
  mingw-w64-ucrt-x86_64-lld

export CC=clang
export CXX=clang++
export CGO_ENABLED=1
export CGO_CXXFLAGS=-I/ucrt64/include
export CGO_LDFLAGS=-L/ucrt64/lib
go build ./cmd/qkc
go test ./...
```

Keep the same environment variables set for `go test`. Do not mix UCRT64
libraries with MSVC or another MSYS2 environment such as MINGW64/CLANG64.

MSVC and MSVC-targeted Clang are not supported for building qkc. Go's Windows
cgo toolchain expects a GCC-compatible MinGW environment, and the official
MSVC LLVM archives use a different static component-library layout. This only
restricts how the compiler executable itself is built; qkc can still emit and
link code for Windows MSVC target triples.

## Docs

- [`docs/guide.md`](docs/guide.md) is the QK language and user guide.

## Contributing

I don't really see a point in accepting contributions at this stage, but you may try I guess, maybe I'll like your changes.

## Versions and releases

I use `nightly-*` releases as short-lived development snapshots and `pre.N`
tags for milestones I consider worth keeping. The language has a strong enough
identity that I no longer intend to make major syntax or semantic changes
without a concrete need, but compatibility is still not guaranteed between
these milestones.

During the pre-release stage I mostly want to build real programs, fix the bugs
they expose, expand the standard library, and improve compilation speed and
tooling. QK may stay in this stage indefinitely as a hobby project. If it ever
makes sense to maintain stable releases, I plan to switch to a
`YY.minor.patch` version scheme.
