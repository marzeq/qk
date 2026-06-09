# qk

This is my work in progress compiler for a custom language I'm designing.

## Rationale

As this is my first compiler project, I wanted to keep things simple and not implement complex language features.

As a result of that, the language is simple and fairly similar to C in levels of complexitly.
However, because it has been designed from the ground up, it decided to include some modern features that
are not present in C but are easy enough to implement for a beginner such as myself.

Because of restricting myself to simple language features, where I took the most
liberty with the design is probably the syntax, which while it is somewhat inspired by
languages like Rust and Go, in many ways it is unique to this language.

My end goal is to reach the same level of usability as C, where any project can feasibly be implemented in this language instead of C.

However, I am not dumb, this is not a "C-killer" like other previously have claimed with their own (ekhm-ekhm V), because I am just a
dumb kid with no prior background in compiler or language design, so I know my limitations.

Because my aim is to create a compiler, not design a language, it does not and probably will never have a formal specification or
anything of that sort, and I will make up the language features as I go along. 

As of writing this, the compiler is still not nearly finished and I have already found the codebase to be very cumbersome to work with
and I see many design flaws I have made along the way, so as soon as I reach a certain level of maturity with this language, I will
either abandon this project or rewrite it from scratch with better design choices, inspired by the mistakes I have in this first attempt.

## Building

```bash
git clone git@github.com:marzeq/qk.git
cd qk
git config core.hooksPath .githooks # if you plan to contribue (so in reality, this is a note to self)

go build ./cmd/qkc # or run 'go run ./cmd/qkc' directly
```
## Supported platforms

- This branch includes a working LLVM textual backend and a native build pipeline that emits per-module LLVM `.ll`,
  compiles them with `clang` and links a final executable. The happy-path (compile -> clang -> link) is implemented and
  used for development and testing.

  The backend is portable: targets supported depend on the available clang toolchain and sysroots on your system (for example
  `x86_64` and `aarch64`). Cross-compilation requires a matching toolchain or a sysroot; see the CLI notes below.

## Dependencies

- Modern Go version
- clang toolchain in your PATH

## LLVM backend & Compiler CLI

- The project ships a working LLVM-based codegen and a small compiler front-end binary: `cmd/qkc`.
- Basic workflow: the compiler lowers source files to LLVM textual IR (`.ll`) per-module, then runs `clang -c` to produce
  object files and finally links them into a single executable with `clang`.

Usage examples

- Build the current workspace and produce an executable named `main`:

```bash
go run ./cmd/qkc -v -O3 -o main .
```

- Cross-compile for ARM64 (requires ARM sysroot/toolchain):

```bash
go run ./cmd/qkc --target aarch64-unknown-linux-gnu --sysroot /path/to/aarch64-sysroot -O3 -o main .
```

Flags of interest

- `-o <path>` / `--output <path>`: output file name (default `a.out`).
- `-O<level>` or `-O <level>`: optimization level (0..3), default `-O2`.
- `--target <triple>`: pass a target triple to clang (useful for cross-compiling).
- `--sysroot <path>`: pass a sysroot to clang/linker for cross-linking.
- `--clang-arg <args>` / `--clang-args <args>`: forward extra args to clang during compilation (LL file -> object).
- `--link-arg <args>` / `--link-args <args>`: forward extra args to clang during linking.
- `--dump-ir`: print the textual IR emitted for each module to stdout.
- `--dump-llvm`: print the final LLVM text emitted (same as dump-ir but for the combined output).
- `--keep-build-dir`: do not remove the temporary build directory containing `.ll` and `.o` files.
- `--static`: pass `-static` to the final linker (when appropriate).

Notes

- Cross-linking requires the target runtime objects (crt*.o) and libraries (libgcc, libc) to be available in the
  specified sysroot or installed toolchain; otherwise linking will fail.


## Language grammar

- See `docs/grammar.md` for an EBNF grammar and parser-accurate syntax notes.

## Contributing

No, this is not a "serious" project, plese don't use or contribute to this.
