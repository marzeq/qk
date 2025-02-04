# quokka

this is my work in progress programming language targeting the [QBE backend](https://c9x.me/compile/)

as this is my first language i aim for:
- no crazy/revolutionary new features
- simple type system
- modern and clean syntax inspired by rust and go

you can find the language specification in [the `grammar` file](./grammar) or look at the current example i have comitted in [`test.qk`](./test.qk)

## roadmap

- [ ] tokeniser:
  - [x] various punctuation tokens
  - [x] keyword/identifier tokens
  - [x] number literals
  - [ ] char literals
  - [ ] string literals
  - [x] boolean literals (true, false)
- [ ] parser:
  - [ ] statements:
    - [x] import statements
    - [x] function definitons
    - [x] variable declarations
    - [x] assigment/increment/decrement/... statements
    - [x] if statements
    - [x] for loops
    - [x] break, continue keywords
    - [x] return keyword
    - [ ] struct definitions
  - [x] expressions:
    - [x] logical operators ('not', 'and', 'or')
    - [x] comparison operators
    - [x] +, - operators
    - [x] *, / operators
    - [x] negation operator
    - [x] all elsewhere supported type literals
    - [x] identifiers
    - [x] function calls 
    - [x] if expressions
- [ ] import/module system:
  - [ ] imports by simple inclusion by filepath during parse time
  - [ ] rewrite solution to use a module system:
    - [ ] resolve imports after parsing
    - [ ] flatten symbols
- [ ] type system:
  - [x] signed and unsigned integers
  - [x] booleans
  - [ ] chars
  - [ ] strings and cstrings
  - [x] void for function return types
  - [ ] structs
- [x] typechecker:
  - [x] statements:
    - [x] function definitons
    - [x] variable declarations
    - [x] assigment/increment/decrement/... statements
    - [x] if statements
    - [x] for loops
    - [x] break, continue keywords
    - [x] return keyword
  - [x] expressions:
    - [x] logical operators ('not', 'and', 'or')
    - [x] comparison operators
    - [x] +, - operators
    - [x] *, / operators
    - [x] negation operator
    - [x] all elsewhere supported type literals
    - [x] identifiers
    - [x] function calls 
    - [x] if expressions
- [ ] QBE IR generation:
  - ... roadmap not defined yet
