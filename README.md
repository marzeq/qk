# quokka

This is my work in progress programming language targeting the [QBE backend](https://c9x.me/compile/)

As this is my first language I aim for:
- No crazy/revolutionary new features
- Simple type system
- Modern and clean syntax inspired by Rust and Go

## Cloning and building

```bash
git clone git@github.com:marzeq/quokka.git
cd quokka
git config core.hooksPath .githooks # please do this, it:
# 1. ensures formatting consistency by blocking your commit if it's misformatted
# 2. runs tests before you push

go build . # or go run . (args...)
```
## Supported platforms

The compiler is limited by QBE which supports:

- Linux (x86-64, ARM64)
- Apple (x86-64, ARM64)

There is a special branch in QBE called `winabi` and I plan to vendor in that branch to support Windows in the future, but not yet.

## Dependencies

- QBE
- Standard POSIX build tools
- (in the future mingw for Windows support)

## Contributing

For now no, maybe in the future.
