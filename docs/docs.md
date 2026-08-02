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

# Ressignments

Mutable variables can be reassigned with the `=` operator:

```qk
x = 10
```

# Unitialised variables

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

**TODO: FINISH DOCS.**
