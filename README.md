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

- Codegen is not yet implemented on this branch. In the main branch we target QBE, so it's only X86-64 and ARM64, but here I plan to
  finally switch over to LLVM, so in the future we should be able to support more platforms.

## Dependencies

- Modern Go version

## Language grammar

- See `docs/grammar.md` for an EBNF grammar and parser-accurate syntax notes.

## Contributing

No, this is not a "serious" project, plese don't use or contribute to this.
