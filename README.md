# qk

This is my work in progress compiler for a custom language I'm designing.

## My rationale

See [below](#rationale).

## Building

```bash
git clone git@github.com:marzeq/qk.git
cd qk
git config core.hooksPath .githooks # if you plan to contribute

go generate ./stdlib # typecheck the embedded QK standard library
go build ./cmd/qkc   # or run 'go run ./cmd/qkc' directly
```
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

#### Distribution packages

The normal build dynamically links LLVM, Clang C++, and LLD. Distribution
packagers should use this mode and declare the appropriate runtime library
dependencies in their package metadata. They should not bundle these libraries:
the package manager is responsible for keeping QK and its LLVM ABI dependencies
compatible and rebuilding QK when necessary.

#### Standalone Linux releases

The release packaging script builds `qkc`, bundles its LLVM, Clang, and LLD
shared libraries, their non-glibc runtime dependencies, and Clang resource
files, configures executable-relative library lookup, and creates a versioned
archive:

```bash
scripts/package-release-linux.sh 0.1.0
```

The script requires `clang`, `ldd`, `realpath`, and `tar`, and writes to `dist/`
unless a second output-directory argument is supplied. Run it in the pinned
Linux environment used for the GitHub release. The bundle does not change the
runtime dependencies of programs produced by the compiler.

#### Standalone macOS releases

The macOS packaging script provides the equivalent relocatable archive using
native Mach-O library paths and ad-hoc code signing:

```bash
scripts/package-release-macos.sh 0.1.0
```

It requires Go, LLVM and LLD development libraries, `otool`,
`install_name_tool`, `codesign`, and `tar`. Homebrew's separate keg-only `llvm`
and `lld` formulae are detected automatically. Other layouts can be selected
with `LLVM_CONFIG`, `LLD_ROOT`, and `CLANG`. Run the script natively on each
supported macOS architecture.

### Using the compiler

- Target CRT objects and native libraries for hosted linking

## Compiler CLI

Use the `-h`/`--help` flag for a full list of options:

```bash
qkc -h
```

### Examples

#### Compiling current directory

```bash
qkc build .
./qk
```

The default executable is named after the selected directory (`qk.exe` on
Windows for this repository).

The selected directory is the primary package. QK reads its immediate `.qk`
files, then resolves imports from the invocation directory without recursively
walking the source tree. For example, `qkc run ./games/flappy` resolves
`vendor.raylib` at `./vendor/raylib`. A package declaring `module main` builds
as an executable; other packages build as relocatable objects by default.

Run a command package directly with `qkc run .`. Arguments following the
package path are forwarded to the generated program.

#### Specifying an output file

```bash
qkc build -o my_program .
```

#### Building a shared library from `foo` module

```bash
qkc build -o libfoo.so foo/
```

Or by specifying the output type explicitly and using the default output name (in this case `libbaz.so`):

```bash
qkc build -t so baz/
```

#### Building an object file into a static library

```bash
qkc build -o foo.o .
ar rcs libfoo.a foo.o
```

#### Cross compiling for arm64 Linux with optimizations

```bash
qkc build -target aarch64-unknown-linux-gnu -sysroot $(aarch64-linux-gnu-gcc -print-sysroot) -O3 .
```

Notes

- The same import-directed package loading rules apply to executables, libraries, and object files.
- Cross-linking requires the target runtime objects (crt*.o) and libraries (libgcc, libc) to be available in the
  specified sysroot or installed toolchain; otherwise linking will fail.


## Docs

- [`docs/docs.md`](docs/docs.md) is the language and compiler reference.

## Contributing

I don't really see a point in accepting contributions at this stage, but you may try I guess, maybe I'll like your changes.

## Rationale

As this is my first compiler project, I wanted to keep things simple and not implement complex language features.

As a result, the language is fairly simple and broadly similar to C in terms of complexity and semantics.
However, because it has been designed from the ground up, I decided to include some modern features that
are not present in C but are easy enough to implement for a beginner such as myself.

Because I restricted myself to relatively simple language features, most of my experimentation has been in the syntax,
which while it is somewhat inspired by languages like Rust and Go, in many ways it is unique to this language.

My end goal is to reach the same level of usability as C, where any project can feasibly be implemented in this language instead of C.

That said, I do not expect this to be a "C killer" as many language projects have claimed to be, because I have
no prior background in compiler or language design. Basically, I know my place.

Because my aim is to create a compiler, not design a language, it does not currently have any formal specification, and the design is very much a work in progress, so I will be making changes to the language design as I go along.
