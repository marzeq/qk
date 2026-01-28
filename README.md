# qk

This is my work in progress compiler targeting the [QBE backend](https://c9x.me/compile/) for a custom language I'm designing.

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
dumb kid with no prior background in compiler design, so I know my limitations.

As of writing this, the language is still not nearly finished and I have already found the codebase to be very cumbersome to work with,
so as soon as I reach a certain level of maturity with this language, I will probably abandon this and move on.

## Cloning and building

```bash
git clone git@github.com:marzeq/qk.git
cd qk
git config core.hooksPath .githooks # if you plan to contribue (so in reality, this is a note to self)

go build ./cmd/qkc # or run 'go run ./cmd/qkc' directly
```
## Supported platforms

The compiler is limited by QBE which supports:

- Linux (x86-64, ARM64)
- Apple (x86-64, ARM64)

There is a special branch in QBE called `winabi` and I plan to vendor-in that branch to support Windows in the future, but not yet.

## Dependencies

- Modern Go version
- QBE
- Standard POSIX build tools
- (in the future mingw for Windows support)

## Contributing

No, this is not a "serious" project, plese don't use this.
