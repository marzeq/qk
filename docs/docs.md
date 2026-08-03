# Hello world

Write the following program:

```qk
module main

import std.io

let main() {
  std.io.println("Hello, world!")
}
```

Save to a `.qk` file, and then compile and run it with the following command:

```bash
qkc run .
# or:
qkc run <filename>.qk
```

You may also just compile it to a binary with:

```bash
qkc build .
# or:
qkc build <filename>.qk
```

# Variables

Variables for the current scope are declared and initialised as follows:

```qk
let x: i32 = 5
```

The type can be ommited if it's clear from the context:

```qk
let s = "Hello, world!" // The type is inferred to be str
```

QK strongly encourages the use of immutable variables, but mutable variables can be declared with the `mut` keyword:

```qk
let mut x: i32 = 5
```

## Ressignments

Mutable variables can be reassigned with the `=` operator:

```qk
x = 10
```

## Unitialised variables

If you wish to declare a variable without initialising it, you can use the special `---` syntax:

```qk
let mut x: i32 = ---
```

This is equivalent to `int x;` in C – not to be confused with `int x = {0};`.

As such, using a `---` variable before it has been initialised is undefined behaviour.

# Literals

## String literals

String literals are enclosed in double quotes and support common escape sequences:

```qk
"Hello, world!"
"Two\nlines"
```

They resolve to a `str` type, which is a slice of bytes. See [the types section](#types) for more information on the difference between `str` and `cstr`.

## C-String literals

C-String literals are prefixed with a `c` and are enclosed in double quotes. They are null-terminated and can be used to interface with C code:

```qk
c"Hello, world!"
c"Two\nlines"
```

They resolve to a `cstr` type, which is a pointer to bytes. See [the types section](#types) for more information on the difference between `str` and `cstr`.

## Character literals

Character literals are enclosed in single quotes and represent a single **ASCII** character:

```qk
'a'
'\t'
```

These resolve to a `u8` type, which is an unsigned 8-bit integer. Note that QK does not support Unicode characters in character literals.

## Numeric literals

Numeric lierals are defined like any other programming language. They can be integers or floating-point numbers:

```qk
let x: i32 = 42
let y: f64 = 3.14
```

Note that numeric literals are polymorphic and assume a concrete type based on the context. For example, `42` can be inferred as `i32`, `i64`, or even `f64` depending on how it is used.

# Comments

QK follows the C/C++-style comment tradition:

```qk
// Single-line comment
/*
  Multi-line
  comment
*/
```

# Modules

QK's module system is inspired by Go's module system - that is, the semantic names of modules follow the directory structure of the project. For example, a files belonging to the `foo.bar` module would live in the `foo/bar` directory. The module name is declared at the top of the file:

```qk
module foo.bar
```

Just like Go, there is a special `main` module that is the entry point of the program. The `main` module must contain a `main()` function, which is the first function to be executed when the program starts. A project can have multiple main modules, and the convention is to put them in a `cmd` directory. For example, a project with two main modules would have the following structure:

```
cmd/
  foo/
    main.qk
  bar/
    main.qk
```

The semantic name to directory structure mapping is done from the current working directory where the compiler is invoked.

A directory cannot have multiple files declaring different modules.

## `import` statement

Packages can be imported with the following `import` statement:

```qk
import std.io // available under `std.io`
import foo.bar.baz baz // available under `baz`

import (
  std.alloc
  std.math
) // multiple imports can be grouped together
```

## Symbol visibility

All symbols by default are not visible outside of the module they are declared in. To make a symbol visible outside of the module, it must be prefixed with the `pub` keyword:

```qk
pub let x: i32 = 42 // visible outside of the module

pub let foo() {
  // ...
} // also visible outside of the module
```

# Control flow

## `for` statement

QK inherits Go's `for` statement, which can be used like:

```qk
for {
  // infinite loop, equivalent to while (true) in C
}

for condition {
  // loop while condition is true, equivalent to while (condition) in C
}

for let mut i: i32 = 0; i < 10; i++ {
  // loop with an initialisation, condition and post statement, equivalent to for (int i = 0; i < 10; i++) in C
}
```

QK also supports range and iteration over supported built-in types, such as arrays, slices and strings:

```qk
for x in 0..10 {
  // loop from 0 to 9, inclusive
}

for x in 0..=10 {
  // loop from 0 to 10, inclusive
}

for elem in sl {
  // loop over each element in slice sl
}

for elem in arr {
  // loop over each element in array arr
}

for ch in s {
  // loop over each character in string s
}

for elem in sl @reversed {
  // loop over the slice from its last element to its first
}

for x in 0..10 @reversed {
  // loop from 9 down to 0
}

for x in 0..=10 @reversed {
  // loop from 10 down to 0
}
```

The postfix `@reversed` iteration attribute is supported on ranges, arrays,
slices and strings. The iterable expression and range bounds are still
evaluated exactly once.

### `break` and `continue`

Just like in any other C-like language, `break` and `continue` can be used to control the flow of loops:

```qk
for let mut i: i32 = 0; i < 10; i++ {
  if i == 5 {
    break // exit the loop when i is 5
  }
  if i % 2 == 0 {
    continue // skip the rest of the loop when i is even
  }
  std.io.println("{}", i)
}
```

**TODO: finish docs.**
