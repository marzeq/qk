# qk

This is my work in progress compiler for a custom language I'm designing.

## Building

```bash
git clone git@github.com:marzeq/qk.git
cd qk
git config core.hooksPath .githooks # if you plan to contribute

go generate ./stdlib # typecheck the QK library tree in ./libs
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

## Supported platforms

### For the compiler itself

Linux, macOS, and Windows hosts are supported when the required LLVM 22,
Clang C++, and LLD development libraries are available. Linux is currently the
most extensively exercised host.

### For compiling code with the compiler

The compiler supports recognised 32-bit and 64-bit target architectures. C ABI
aggregate lowering is currently limited to the documented target families.

## Dependencies

### Building the compiler

- Reasonably modern Go version
- A C++17 compiler
- LLVM and Clang 22 development headers
- LLVM 22, Clang C++, and LLD driver libraries

The headers and libraries must be visible to cgo's C++ compiler and linker. On
Windows, use an LLVM build compatible with the selected cgo toolchain and set
`CGO_CXXFLAGS`/`CGO_LDFLAGS` when it is installed outside standard search paths.

## Docs

- [`docs/guide.md`](docs/guide.md) is the QK language and user guide.

## Contributing

I don't really see a point in accepting contributions at this stage, but you may try I guess, maybe I'll like your changes.
