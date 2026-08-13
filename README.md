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

Linux, macOS, and Windows hosts are supported when the required LLVM 22
development libraries are available. Final linking uses platform tools from
`PATH`. Linux is currently the most extensively exercised host.

### For compiling code with the compiler

The compiler supports recognised 32-bit and 64-bit target architectures. C ABI
aggregate lowering is currently limited to the documented target families.

## Dependencies

### Running a bundled release

Release builds embed LLVM statically. Linux release executables also statically
link their non-system compiler runtime dependencies. The stock MSYS2 LLVM
archives retain their zstd DLL ABI, so Windows releases place `libzstd.dll`
beside `qkc.exe`; macOS does not support fully static executables, so its
release depends only on Apple system libraries. Functionally, a release needs
only `bin/` and `libs/`.
`LICENSE`, `LLVM-LICENSE.txt`, and any bundled runtime license such as Windows'
`ZSTD-LICENSE.txt` are included as distribution metadata; additional notices
may be required if the selected static LLVM build pulls in other third-party
libraries.
Native final links additionally require the platform toolchain described below.

### Building the compiler yourself

All source builds require Go 1.23.5 or newer, cgo, a C++17 compiler, and a
matching LLVM 22 development installation containing:

- LLVM headers under `llvm/`; and
- the LLVM shared library.

LLVM's command-line tools alone are insufficient. The headers and libraries
must match the selected C++ ABI and be visible through `CGO_CXXFLAGS` and
`CGO_LDFLAGS`.

Ordinary `go build ./cmd/qkc` deliberately links against the system's shared
LLVM. The release scripts use the `qk_static_llvm` build tag and require LLVM's
static component archives plus static versions of their non-system
dependencies; they fail instead of falling back to a dynamic release.

#### Linux

Install Go, Clang/Clang++, LLVM 22 development packages and an archiver. Package names vary.

```bash
llvm_prefix=$(llvm-config --prefix) # may need to be llvm-config-22
export CC=clang # may need to append -22
export CXX=clang++ # also may need to append -22
export CGO_ENABLED=1
export CGO_CXXFLAGS="-I${llvm_prefix}/include"
export CGO_LDFLAGS="-L${llvm_prefix}/lib"
go build ./cmd/qkc
go test ./...
```

Keep the same environment variables set for `go test`.

#### macOS

Install the Xcode Command Line Tools, Go, and LLVM 22. With Homebrew:

```bash
xcode-select --install
brew install go llvm
source ./scripts/macos-brew-init.sh
go build ./cmd/qkc
go test ./...
```

The initialization script locates the Homebrew LLVM prefix and exports the cgo
include and library paths.

#### Windows (MSYS2 UCRT64)

Run these commands in an MSYS2 UCRT64 shell. Keep all components in the same
UCRT64 ABI environment:

```bash
pacman -S --needed \
  mingw-w64-ucrt-x86_64-go \
  mingw-w64-ucrt-x86_64-clang \
  mingw-w64-ucrt-x86_64-llvm

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

MSVC and MSVC-targeted Clang are not supported for building qkc itself. Go's
Windows cgo toolchain expects a GCC-compatible MinGW environment, and the official
MSVC LLVM archives use a different static component-library layout. This only
restricts how the compiler executable itself is built. QK's default Windows
target remains MSVC and final links use `link.exe`/`lib.exe` from an initialized
Visual Studio developer environment. Explicit Windows GNU targets use Clang
with the supplied MinGW sysroot.

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
