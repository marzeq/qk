<!-- This is the user-facing QK language guide. Do not document compiler internals, implementation details, cache formats, or internal build architecture here. -->

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

The type can be omitted if it is clear from the context:

```qk
let s = "Hello, world!" // The type is inferred to be str
```

QK strongly encourages the use of immutable variables, but mutable variables can be declared with the `mut` keyword:

```qk
let mut x: i32 = 5
```

## Reassignments

Mutable variables can be reassigned with the `=` operator:

```qk
x = 10
```

## Uninitialised variables

If you wish to declare a variable without initialising it, you can use the special `---` syntax:

```qk
let mut x: i32 = ---
```

This is equivalent to `int x;` in C – not to be confused with `int x = {0};`.

As such, using a `---` variable before it has been initialised is undefined behaviour.

## Shadowing and discards

A local declaration may shadow an earlier declaration in the same lexical scope. Its initializer is resolved before the new binding becomes visible:

```qk
let value: i32 = 10
let value = value + 1
```

The name `_` explicitly discards a value and does not create a binding. It is most often used while unpacking multiple function results.

# Literals

## String literals

String literals are enclosed in double quotes and support common escape sequences:

```qk
"Hello, world!"
"Two\nlines"
```

An ordinary string literal is contextually typed as either `str` or `cstr`. It becomes a null-terminated `cstr` when used where that type is expected, including function arguments, return values, declarations, assignments, and aggregate fields. Without such context it defaults to `str`:

```qk
let text = "length-aware"       // str
let name: cstr = "terminated"   // cstr
let file = fopen("input.txt", "rb")
```

See [the types section](#types) for more information on the difference between `str` and `cstr`.

## C-String literals

C-String literals are prefixed with a `c` and are enclosed in double quotes. They are null-terminated and can be used to interface with C code:

```qk
c"Hello, world!"
c"Two\nlines"
```

They always resolve to a `cstr` type, which is a pointer to bytes. The explicit prefix remains useful when no expected type is available or when the spelling should document C-string intent. See [the types section](#types) for more information on the difference between `str` and `cstr`.

## Character literals

Character literals are enclosed in single quotes and represent a single **ASCII** character:

```qk
'a'
'\t'
```

These resolve to a `u8` type, which is an unsigned 8-bit integer. Note that QK does not support Unicode characters in character literals.

## Numeric literals

Numeric literals are written much like they are in other programming languages. They can be integers or floating-point numbers:

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

QK's module system is inspired by Go's module system—that is, semantic module names normally follow the directory structure of the project. Optional project source mounts can add an import prefix without changing a library's semantic module names. The module name is declared at the top of the file:

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

Semantic names are mapped to the directory structure relative to the current working directory.

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

for let mut i: i32 = 0; i < 10; i += 1 {
  // loop with an initialisation, condition and post statement
}
```

QK also supports range and iteration over supported built-in types, such as arrays, slices and strings:

```qk
for x in 0..10 {
  // loop from 0 through 9
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

for elem, index in sl {
  // index has type usz and is the position of elem in sl
}

for _, index in s {
  // discard each byte and use only its index
}

for elem, _ in sl {
  // explicitly discard the index
}

for elem.&, index in sl {
  // elem is *T and points to the element at index
}

for elem.&mut in mutable_sl {
  // elem is *mut T; mutable_sl must have type []mut T
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
evaluated exactly once. Array, slice and string iteration may bind an optional
second `usz` index after the element binding. Either binding may be `_`; under
`@reversed`, the index remains the element's position in the original sequence.
Slice and string element bindings may append `.&` to bind an immutable pointer
to each element. Mutable slices additionally support `.&mut`, which binds a
`*mut T`; pointer element bindings are not supported for array iteration because
arrays are copied into the loop's iterable storage.

### `break` and `continue`

Just like in any other C-like language, `break` and `continue` can be used to control the flow of loops:

```qk
for let mut i: i32 = 0; i < 10; i += 1 {
  if i == 5 {
    break // exit the loop when i is 5
  }
  if i % 2 == 0 {
    continue // skip the rest of the loop when i is even
  }
  std.io.println("{}", i)
}
```

## `if` statement

An `if` condition must have type `bool`. Parentheses around the condition are not used:

```qk
if temperature < 0 {
  std.io.println("freezing")
} else if temperature < 20 {
  std.io.println("cool")
} else {
  std.io.println("warm")
}
```

An `if` may also produce a value. Every path must then yield the same type, and an `else` branch is required:

```qk
let description = if temperature < 0 {
  "freezing"
} else {
  "not freezing"
}
```

The final expression in each branch is its value.

## Blocks as expressions

A block used where an expression is expected yields its final expression. This is useful when calculating a value takes a few local steps:

```qk
let area: i32 = {
  let width: i32 = 8
  let height: i32 = 5
  width * height
}
```

Ordinary statement blocks do not produce values. A value-producing function body, introduced with `=`, may use a block expression in the same way.

## `match`

`match` selects an arm by comparing a subject with patterns. Enum variants use a leading dot, `_` is the wildcard, `|` joins alternatives, and `if` adds a runtime guard:

```qk
let label = match status {
  .Ready | .Running => "active",
  .Stopped if retry => "retrying",
  .Stopped => "stopped",
}
```

A match over an enum or tagged union must be exhaustive. Scalar matches can use integer, character, boolean, string, and C-string literals, as well as half-open integer or character ranges:

```qk
match byte {
  '0'..':' => std.io.println("digit"),
  'a'..'{' | 'A'..'[' => std.io.println("letter"),
  _ => std.io.println("other"),
}
```

Range patterns are half open, just like `start..end` loops, so the punctuation bounds above are the bytes immediately after `9`, `z`, and `Z`.

Like `if`, `match` works as either a statement or an expression. Its subject is evaluated once.

`as` gives that one evaluated subject a local name shared by the arms:

```qk
match next_value() as value {
  0 => handle_zero(value),
  _ => handle_other(value),
}
```

A branch that returns or otherwise terminates control flow does not need to contribute a value to an expression match.

## `return`

`return` immediately leaves the current function. A result-bearing function returns a value of its declared result type:

```qk
let absolute(value: i32): i32 {
  if value < 0 {
    return -value
  }
  return value
}
```

A `void` function may use a bare `return`. Calls returning `void` can be statements, but `void` itself is not a storable value.

## `defer`

`defer` schedules a call or block for the end of the current scope. Deferred actions run in last-in, first-out order, including when the function returns early:

```qk
let mut file, error = std.io.FileReader.open_checked(path)
if !error.is_none() {
  return
}
defer file.close()
```

A deferred block is written `defer { ... }`. Values used by the action remain ordinary QK expressions; `defer` changes when the action runs.

# Operators and expressions

QK has the usual arithmetic operators `+`, `-`, `*`, `/`, and `%`. Comparisons use `==`, `!=`, `<`, `<=`, `>`, and `>=`, while boolean logic uses `!`, `&&`, and `||`.

Integer bit operations use `&`, `|`, `^`, `~`, `<<`, and `>>`. The compound assignments `+=`, `-=`, `*=`, `/=`, `%=`, `&=`, `|=`, `^=`, `<<=`, and `>>=` update a mutable place.

QK deliberately has no increment or decrement expressions. Write `index += 1` or `index -= 1` instead of `index++` or `index--`.

Operators follow conventional precedence. Parentheses may be used whenever the intended grouping should be explicit:

```qk
let visible = enabled && (permissions & 0x4) != 0
```

An expression used as a statement must normally be a function call. Assigning to `_` explicitly discards another result and documents that the value is intentionally unused.

# Functions

A function is a top-level `let` binding followed by a parameter list. Parameters and results are typed:

```qk
let add(left: i32, right: i32): i32 {
  return left + right
}
```

Parameters with the same type may share one annotation:

```qk
let add(left, right: i32): i32 = left + right
```

The second form also demonstrates an expression body. A function introduced with `=` implicitly returns its expression, while a normal `{ ... }` body uses explicit `return` statements.

Parameters are immutable bindings by default. Prefix a parameter with `mut` when its local binding must be reassigned; this does not by itself make a pointed-to value mutable.

## Default parameters

A trailing parameter may provide a default expression:

```qk
let repeat(value: str, count: usz = 1): void {
  // ...
}

repeat("hello")
repeat("hello", 3)
```

Defaults are evaluated in the declaring function's scope. They are call conveniences and do not change the underlying function type or ABI.

## Multiple results

A function may return a parenthesised list of two or more result types:

```qk
let divide(left: i32, right: i32): (i32, bool) {
  if right == 0 {
    return 0, false
  }
  return left / right, true
}

let quotient, valid = divide(10, 2)
```

Multiple results are unpacked directly from one call. They are not a general-purpose tuple value. A call returning several results can also be returned directly or assigned into an equally sized target list:

```qk
quotient, valid = divide(20, 4)
let _, found = lookup(key)
```

`let mut a, b = call()` makes every non-discard binding mutable. `_` never creates a binding.

## Typed variadics

A typed variadic parameter accepts zero or more values of one type:

```qk
let sum(values: ...i32): i32 {
  let mut total: i32 = 0
  for value in values {
    total += value
  }
  return total
}

let total = sum(2, 3, 5)
```

Inside the function, `values` is an immutable slice. An existing slice can fill the final variadic position with `sum(values...)`.

The bare `...` parameter is reserved for C-style variadic foreign functions. Those arguments retain the platform C ABI and are not packaged into a slice.

## Function pointers

A function pointer names its parameter and result types but not its parameter names:

```qk
let operation: *(i32, i32): i32 = add
let result = operation(2, 3)
```

Function pointers can be passed and stored like other pointer values. Their complete signatures, including typed variadics, must match.

## Lambda expressions

A non-capturing lambda is an anonymous function value written with `|...| =>`:

```qk
let increment: *(i32): i32 = |value| => value + 1
let multiply = |left, right: i32| => {
  left * right
}
```

Parameter annotations may be omitted when an expected function-pointer type supplies them, such as in an annotated declaration, argument, assignment, or return. Otherwise every parameter needs a type annotation. A lambda has no default parameters and cannot refer to local variables or parameters from an enclosing function; module globals and named functions remain available.

Typed variadic lambdas use the same `...T` parameter and slice-backed calling convention as named functions:

```qk
let sum_values = |values: ...i32| => sum(values)
let contextual: *(...i32): i32 = |values| => sum(values)
```

# Types

QK is statically typed. Inference removes annotations when context determines one concrete type, but it does not make a binding dynamically typed.

## Boolean type

`bool` has the two values `true` and `false`. Conditions require `bool`; integers are not implicitly treated as conditions.

## Integer types

The signed fixed-width integers are `i8`, `i16`, `i32`, and `i64`. Their unsigned counterparts are `u8`, `u16`, `u32`, and `u64`.

`isz` and `usz` are signed and unsigned integers with the target's pointer width. Sizes, alignments, lengths, and sequence indices use `usz`.

Integer literals may be decimal, binary, octal, or hexadecimal:

```qk
let decimal: i32 = 42
let binary: u8 = 0b101010
let octal: u16 = 0o52
let hexadecimal: u32 = 0x2a
```

Widening within the same signedness is implicit, as is conversion from an integer to a suitable floating-point type. Conversions that can lose information or change signedness should use an explicit cast.

## Floating-point types

`f32` and `f64` are the 32-bit and 64-bit floating-point types. Decimal floating-point literals require digits on both sides of the dot, such as `3.0`; `.5` and `5.` are not floating-point literals.

## `void`

`void` marks a function that returns no value. It cannot be used for a variable, parameter value, aggregate field, or other storage.

Pointers to `void` are permitted and are useful at foreign and allocation boundaries. They still carry no information about the pointed-to representation.

## Defined types and aliases

`type` creates a new nominal type. Even when its representation is another type, it remains distinct and requires an explicit cast to cross that boundary:

```qk
let UserId = type u64

let raw: u64 = 42
let user: UserId = raw.(UserId)
```

`type alias` creates a transparent alias instead:

```qk
let Byte = type alias u8
```

Aliases and defined types may be parameterized by compile-time types. Defined parameterized types have distinct identities for each concrete argument list, while a transparent alias retains the identity of its substituted target.

## Arrays

An array `[N]T` stores exactly `N` values of `T` inline. Its length is part of its type, and assigning or passing an array copies its elements:

```qk
let numbers: [4]i32 = [10, 20, 30, 40]
let zeros: [8]u8 = [0; 8]
```

The repeated form `[value; count]` evaluates `value` once and copies it into every element. A sequence literal needs an expected array or slice type to determine its representation.

Arrays are indexed with `array[index]`. A statically known invalid array index is rejected, but ordinary runtime indexing is unchecked; use slicing when a runtime bounds check is required.

Local repeated literals may use `---` to leave their element storage unwritten:

```qk
let mut buffer: [4096]u8 = [---; 4096]
```

Reading an element before writing it is undefined behaviour. Module-scope arrays remain zero-initialised.

## Slices

A slice is a pointer-and-length view over contiguous elements. `[]T` is an immutable view, while `[]mut T` permits element mutation:

```qk
let values: []i32 = [1, 2, 3]
let mutable_values: []mut i32 = [1, 2, 3]

mutable_values[0] = 10
```

Slice element capability is separate from binding mutability. `let mut view: []i32` permits replacing the slice descriptor but still does not permit `view[0] = value`.

A mutable slice coerces to an immutable slice. The reverse conversion is forbidden because immutability cannot be discarded.

Slice indexing is also unchecked. The length is available for explicit validation, while slicing an array or slice validates its complete range. Pointer slicing can check only that its start does not exceed its explicit end because a pointer carries no allocation length.

Use `@len(value)` to obtain the `usz` length of an array, slice, or string:

```qk
for index in 0..@len(values) {
  std.io.println("{}", values[index])
}
```

## Slicing

`subject[start:end]` creates a zero-copy view whose start is inclusive and whose end is exclusive. Either bound may be omitted when the subject has a known length:

```qk
let middle = values[1:3]
let prefix = values[:2]
let suffix = values[2:]
let all = values[:]
```

The subject and explicit bounds are each evaluated once. QK checks `start <= end <= @len(subject)` at runtime when it cannot prove the bounds statically.

Arrays implicitly borrow as slices when an expected slice type requests it. An immutable pointer can form `pointer[0:count]`, and a mutable pointer forms a mutable slice; pointers require an explicit end because they carry no length.

Slicing a `str` produces another `str`. Slicing a mutable slice preserves its element-write capability.

## Strings and C strings

`str` is QK's nominal immutable byte string. It uses a slice-like pointer-and-length representation, may contain zero bytes, and does not promise null termination.

`cstr` is an immutable `*u8` representation intended for null-terminated C strings. It has no stored length and is normally traversed until `\0`.

Neither type implies Unicode text processing. String iteration and indexing operate on bytes, and a character literal is a single `u8` ASCII byte.

## Pointers and references

`*T` is an immutable data pointer and `*mut T` permits mutation through the pointer. Both pointer kinds can hold `nil`:

```qk
let pointer: *i32 = value.&
let mutable_pointer: *mut i32 = value.&mut

let copy = pointer.*
mutable_pointer.* = 7
```

References and dereferences use postfix syntax: `value.&`, `value.&mut`, and `pointer.*`. Field and index access automatically follow pointers where the operation is unambiguous, so `pointer.field` is usually preferable to spelling out a dereference.

Taking `.&mut` requires a mutable place. Mutation validation follows the whole access chain, and every pointer crossed on the way to the destination must be `*mut`.

Pointer casts may change the pointed-to type when an explicit low-level conversion is needed. `*void` and `*mut void` serve as representation-erased data pointers, but dereferencing `void` is invalid.

## `nil`

`nil` is the zero value of data pointers and dynamic trait pointers. It receives its concrete pointer type from context:

```qk
let next: *Node = nil

if next == nil {
  // ...
}
```

For a dynamic trait pointer, comparison with `nil` tests the erased data pointer. An erased typed null pointer therefore also compares equal to `nil`.

## Structs

A struct groups named fields in declaration order:

```qk
let Point = type struct {
  x: i32,
  y: i32,
}
```

A named struct literal uses `Type.{ ... }`. Field initialisers use `=`, not `:`:

```qk
let point = Point.{
  x=10,
  y=20,
}
```

When an expected struct type is already known, the shorter `.{ ... }` form is preferred:

```qk
let origin: Point = .{ x=0, y=0 }
```

Omitted fields are zero-initialised. A final `---` entry suppresses that initialisation for all remaining fields and leaves them unwritten:

```qk
let partial: Point = .{
  x=10,
  ---,
}
```

This form is intended for carefully controlled low-level code. Module-scope storage is always zero-initialised.

### Packed structs

Place `@packed` after `struct` to remove implicit field and tail padding:

```qk
let Header = type struct @packed {
  kind: u8,
  length: u32,
}
```

A packed struct has byte alignment and may contain unaligned fields. QK generates unaligned accesses and follows the target C ABI when a packed value crosses a foreign boundary.

## Enums

An enum is a nominal set of named integer variants:

```qk
let Direction = type enum {
  North,
  East,
  South,
  West,
}

let direction: Direction = .North
```

Implicit values start at zero and increase in declaration order. Alternatively, every member may have an explicit integer value:

```qk
let ExitCode = type enum {
  Success=0,
  Usage=64,
  Failure=1,
}
```

Implicit and explicit members cannot be mixed in one enum. A qualified value such as `Direction.North` may be used when context does not already identify the enum.

## Flags

A flags type is a nominal fixed-width integer mask. Its backing type must be a fixed-width integer:

```qk
let Permission = type flags(u8) {
  Read,
  Write,
  Execute,
}

let mut access = Permission.{ .Read, .Write }
```

Implicit members receive consecutive bits beginning at `1 << 0`. An explicitly valued declaration must give every member a value, using hexadecimal values, `1 << bit`, earlier members, or `|` compositions:

```qk
let OpenMode = type flags(u16) {
  Read=0x1,
  Write=0x2,
  ReadWrite=Read | Write,
}
```

`value.Member` tests whether a member's complete mask is set. Assigning a boolean to that field-map spelling sets or clears the mask:

```qk
if access.Execute {
  run()
}

access.Write = false
```

Flags retain unknown backing bits. They cross the C ABI as their declared integer representation.

## Untagged unions

An untagged union overlays its fields in one storage location:

```qk
let NumberBits = type union {
  integer: u32,
  floating: f32,
}
```

The programmer is responsible for knowing which field is meaningful. Untagged unions are principally a low-level representation and interoperation feature.

They use the same aggregate literal syntax as structs:

```qk
let bits = NumberBits.{ integer=0x3f800000 }
```

An anonymous union may appear directly inside a struct. Its fields are promoted into the containing struct's field namespace while sharing storage.

## Tagged unions

A tagged union pairs a variant tag with optional variant-specific data. An auto-tagged union asks QK to synthesize the private tag:

```qk
let Result = type union(@auto) {
  Ok(i32),
  Error(message: str),
  Cancelled,
}

let success = Result.Ok(42)
let failure: Result = .Error("not found")
let cancelled = Result.Cancelled
```

Payload fields may be positional or named. A contextual constructor such as `.Ok(42)` is valid when the expected union type is known.

`match` safely selects a variant and binds its payload:

```qk
match result {
  .Ok(value) => std.io.println("value: {}", value),
  .Error(message=text) => std.io.println("error: {}", text),
  .Cancelled => std.io.println("cancelled"),
}
```

For positional payloads, bindings follow declaration order. For named payloads, `field=binding` renames a binding; the shorter `field` binds it under its field name.

An explicitly tagged union names an enum whose variants correspond exactly to the union variants:

```qk
let TokenKind = type enum {
  Integer,
  Name,
  End,
}

let Token = type union(TokenKind) {
  Integer(value: i64),
  Name(str),
  End,
}
```

Explicitly tagged unions can expose their structural tag-and-payload representation through `@repr(value)` and `@reprof(Type)`. `@repr` returns a representation-preserving value copy, or an immutable pointer view when given an immutable pointer:

```qk
let raw: @reprof(Token) = @repr(token)
```

Positional raw payload fields are named `_0`, `_1`, and so on. Auto-tagged unions deliberately expose neither a public tag nor raw representation access.

Tagged unions cannot cross the C ABI directly, including through nested aggregate fields. Use the explicit `@reprof(T)` representation at that boundary when its layout is part of the interface.

## Opaque types

An opaque type declares a name whose size and fields are unavailable:

```qk
pub let FILE = type opaque
pub let fopen(path: cstr, mode: cstr): *FILE @foreign
```

Opaque values may only be used behind pointer indirection. Any operation that needs their by-value layout is rejected.

# Methods

A method is a function attached to an owner type. Its first parameter is `self`, `*self`, or `*mut self`:

```qk
let Counter = type struct {
  value: i32,
}

let Counter.get(self): i32 = self.value

let Counter.increment(*mut self): void {
  self.value += 1
}
```

Value receivers receive a copy. `*self` permits access through an immutable pointer, and `*mut self` permits mutation.

Methods use ordinary call syntax:

```qk
let current = counter.get()
counter.increment()
```

A static method omits the receiver and is called through the owner:

```qk
let Counter.init(value: i32): Counter = .{ value=value }

let counter = Counter.init(10)
```

Ordinary modules may attach methods only to nominal types they own. The trusted standard library also supplies methods for primitive types, pointers, slices, `str`, and `cstr`.

## Methods on type-producing and structural owners

A parameterized nominal owner uses a parenthesised owner pattern. `$T` captures the concrete type argument from the receiver:

```qk
let Box($T: type) = type struct {
  value: T,
}

let (Box($T)).get(self): T = self.value
```

The same syntax describes standard-library methods on structural types:

```qk
let ([]$T).first(self): T = self[0]
let (*mut $T).write(self, value: T): void { self.* = value }
```

Owner captures form the leading part of the method's compile-time type parameter list in first-appearance order. A call normally infers them from the receiver or from typed value parameters.

Static calls can qualify the owner as `Box(i32).init(...)`. When a method-specific type cannot be inferred, supply it before the runtime arguments.

# Compile-time type parameters

Functions, methods, and type-producing bindings declare type parameters in their ordinary argument lists. `$` marks a compile-time type binder:

```qk
let identity($T: type, value: T): T = value

let Pair($T: type, $U: type) = type struct {
  first: T,
  second: U,
}
```

Calls may infer type arguments structurally from typed arguments:

```qk
let value: i32 = identity(42)
```

The same parameter can instead be supplied explicitly before runtime arguments:

```qk
let allocate($T: type, count: usz): []mut T {
  return []
}
let bytes = allocate(u8, 16)
let pair: Pair(i32, str) = .{ first=1, second="one" }
```

Untyped numeric literals do not choose an arbitrary default type for an otherwise unresolved parameter. Add an expected type or an explicit type argument when inference has no concrete evidence.

Inference first uses typed arguments, then an expected result type from a declaration, assignment, return, or enclosing call. Result context fills only still-unresolved parameters and never overrides explicit or argument-derived choices.

Parameterized tagged-union owners may be inferred from typed payloads when every owner parameter appears there. Otherwise, qualify the constructor with the required arguments.

## Constraints

A type parameter may require a trait:

```qk
let maximum($T: type(std.PartialOrd), left: T, right: T): T {
  if left.gt(right) {
    return left
  }
  return right
}
```

The parameterized body is checked once under the symbolic constraint, so it may use only the operations that the constraint guarantees. Each concrete specialization keeps `T` in exactly the declared by-value or pointer representation.

Constraints use structural conformance. A type satisfies a trait when its method set has compatible methods, even when the implementing methods are private.

Parameterized bindings use function-like semantics: their compile-time arguments select a concrete specialization, and an expression body may produce either a runtime value or a compile-time result.

# Traits

A trait declares a required method surface:

```qk
let Writer = type trait {
  let write(*mut self, bytes: []u8): usz
}
```

Traits are structural: a nominal type implements `Writer` by defining the required method with the same receiver and signature. There is no separate `implements` declaration.

A requirement may provide a default block or expression body. `Self` is available throughout the body, and the symbolic `self` value exposes only methods declared by the trait:

```qk
let Measured = type trait {
  let measure(self): usz
  let double_measure(self): usz = self.measure() * 2
}
```

A type may omit `double_measure` and inherit the default, or declare a matching method to override it. An explicitly declared same-name method with an incompatible receiver or signature still prevents conformance.

`Self` refers to the concrete implementing type within a trait:

```qk
let PartialEq = type trait {
  let eq(self, other: Self): bool
}
```

Trait requirements may themselves have compile-time type parameters and may constrain them. Traits that use `Self` outside the receiver or declare parameterized methods can be used as static constraints and views, but not as dynamic trait objects.

## `Any`

`Any` is the built-in universal trait. A concrete pointer can be erased to `*dyn Any` or `*mut dyn Any` without defining methods, then checked-cast back to a concrete type or recast to another compatible dynamic trait.

The formatting library uses `*dyn Any` to accept heterogeneous arguments. Contextual immutable conversion lets ordinary values passed to those arguments be borrowed or materialised automatically.

## Static trait views

`Trait`, `*Trait`, and `*mut Trait` cast targets create static, representation-preserving views. Dispatch remains direct because the implementing type is known:

```qk
let pointer_view = value.&mut.(*mut Writer)
```

A value view exposes value receivers. An immutable pointer view also exposes immutable pointer receivers, while a mutable pointer view exposes all receiver forms.

An addressable value can be borrowed automatically when cast to a static pointer view. These static views are distinct from erased `dyn` trait pointers.

## Dynamic trait pointers

`*dyn Trait` and `*mut dyn Trait` are dynamic trait pointers. They provide runtime polymorphism:

```qk
let writer: *mut dyn Writer = file.&mut.(*mut dyn Writer)
writer.write("hello")
```

An explicit concrete-to-dynamic conversion requires a pointer operand. Use `value.&.(*dyn Trait)` or `value.&mut.(*mut dyn Trait)` rather than casting the value directly.

When a function expects an immutable dynamic trait pointer, QK may contextually borrow an addressable value or materialise a computed value into temporary storage. A mutable dynamic trait pointer always requires a mutable place.

Dynamic trait-to-trait casts select a compatible target trait implementation at runtime.

# Casts

An explicit cast is a postfix expression of the form `value.(Type)`:

```qk
let narrow = wide.(u8)
let pointer = address.(*mut Header)
```

Valid numeric conversions, defined-type conversions, pointer conversions, slice/pointer conversions, and supported trait conversions use this spelling. Ordinary invalid single-result casts are compile-time errors.

A cast unpacked into two targets is a checked assertion. It returns the requested value and a success boolean:

```qk
let concrete, ok = trait_pointer.(Widget)
```

The same cast spelling becomes checked from its two-result context. When the conversion is statically impossible, it produces the target's zero value and `false`; a corresponding trapping runtime assertion in single-result form calls `panic` on failure.

Slice-to-array casts require an exact runtime length and copy the elements:

```qk
let array, ok = slice.([4]u8)
```

The single-result form traps on a length mismatch. The checked form returns a zero array and `false`.

# Built-in operations

Compiler builtins use a leading `@` and call-like syntax. They are not ordinary functions and may accept types where a function would require values.

## Inline assembly

`@asm` embeds target-specific LLVM inline assembly. Its ordered entries declare every output, input, and clobber visible to the optimizer:

```qk
let low, high = @asm(
  "rdtsc",
  out u32 "={ax}",
  out u32 "={dx}",
  volatile)
```

Outputs use an explicit QK type and an LLVM output constraint beginning with `=`. Inputs contain an evaluated QK expression followed by an LLVM input constraint, and may refer to earlier outputs with tied constraints such as `"0"`. Outputs must precede inputs, which must precede clobbers:

```qk
let sum = @asm(
  "add $2, $0",
  out u64 "=r",
  in left "0",
  in right "r",
  clobber "cc")
```

One output makes the asm expression produce that type. Two or more outputs produce a multiple-result bundle and must be unpacked like a multiple-returning call. With no outputs, `@asm` produces `void` and may be used as a statement. `volatile` preserves assembly whose effects are not completely represented by its outputs. Output-free assembly and assembly with clobbers are also treated as side-effecting automatically.

Clobber names omit LLVM's surrounding `~{...}` syntax; for example, `clobber "memory"` tells the optimizer that arbitrary memory may be read or written. This is a compiler memory barrier, not a processor memory fence. Constraints and register names are target-specific, and incorrect assembly or an incomplete operand/clobber declaration can still cause invalid code or miscompilation.

## Layout and length

`@sizeof(T)` and `@alignof(T)` return the target-specific byte size and alignment as `usz`. Both also accept a value expression without evaluating it for side effects solely to discover its type:

```qk
let bytes = @sizeof(Header)
let alignment = @alignof(Header)
```

`@offsetof(T, field)` returns the byte offset of a struct field. It is especially useful when checking foreign layouts.

`@len(value)` returns the length of an array, slice, or string. Array lengths are compile-time constants; slice and string lengths come from their descriptors.

## Representation access

`@repr(value)` and `@reprof(T)` expose the structural representation of an explicitly tagged union. They preserve bits and layout rather than performing a conversion.

These builtins are intentionally unavailable for auto-tagged unions. Mutable pointer views are also rejected because raw mutation could violate the relationship between tag and payload.

## `panic` and `assert`

`panic(message)` is a built-in terminating function that accepts `str` and is available in `-nolibc` builds.

`assert(condition, message)` accepts a `bool` and a `str`. It returns normally when the condition is true; otherwise it prints `assertion failed: ` followed by the message and terminates the process.

# Compile-time selection

QK has an explicit compile-time stage for target and configuration selection. Compile-time bindings, `when` branches, and metadata collections can change declarations, values, types, and native link requirements.

## Compile-time values

Prefix a binding name with `$` to require compile-time evaluation:

```qk
let $BufferSize: usz = 4 * 1024
let $HasFiles = !NoHasFiles
```

Compile-time values may be boolean or integer. They have no persistent addressable runtime storage and are materialised as constants at each runtime use; a temporary is created only when a consuming operation requires an address. Untyped integer bindings remain untyped until their use supplies a concrete numeric type.

Module-level compile-time bindings may be public and referenced through imported module names. Local bindings are visible in lexical source order.

Parameterized calculations use the same `$` binder syntax:

```qk
let StorageSize($T: type) = @sizeof(T)
```

Each concrete argument list produces an independently specialized binding. There is no separate initializer keyword: `$` consistently marks compile-time bindings and parameters.

## `when`

`when` chooses one branch during compile-time evaluation:

```qk
when OS == .Windows {
  let Separator: u8 = '\\'
} else {
  let Separator: u8 = '/'
}
```

It can select a complete declaration, statement, or expression. `else when` chains are supported. It cannot insert fragments such as part of a parameter list, argument list, sequence literal, or individual attribute entry.

The built-in configuration values are:

- `OS`, with variants such as `.Windows`, `.Linux`, `.MacOS`, and `.WASI`.

- `Arch`, with variants `.X86`, `.X86_64`, `.ARM32`, `.AArch64`, `.Wasm32`, and `.Wasm64`.

- `Environment`, including `.GNU`, `.MSVC`, `.Musl`, and `.Unknown`.

- `ReleaseMode`, either `.Debug` or `.Release`.

- `PointerBits`, an integer target pointer width.

- `CCharSigned`, whether plain C `char` is signed for the target ABI.

Compile-time expressions support booleans, integers, the configuration enum values, logical operations, integer arithmetic and comparison, and references to visible compile-time bindings.

## Compile-time errors

`@compiler_error("message")` stops compilation when expansion selects it:

```qk
when PointerBits != 32 && PointerBits != 64 {
  @compiler_error("unsupported pointer width")
}
```

Unselected directives have no effect. This makes the directive useful for rejecting unsupported target configurations.

`@compiler_assert(condition, "message")` evaluates its condition during the same compile-time stage. A false condition stops compilation with `compiler assertion failed: ` followed by the message, while a true assertion contributes no runtime code:

```qk
@compiler_assert(PointerBits == 64, "this package requires a 64-bit target")
```

# Attributes and foreign interfaces

Attributes follow the declaration they affect, except `@packed`, which follows `struct`. They begin with `@` and use named string options where applicable.

## Foreign declarations

`@foreign` declares a function or global provided by another object or library. It has no QK body:

```qk
let puts(text: cstr): i32 @foreign
let errno: i32 @foreign(symbol "errno")
```

The default symbol is the QK binding name and the default ABI is C. Both can be selected explicitly:

```qk
let callback(value: i32): void
  @foreign(abi "qk", symbol "package.callback")
```

Use `abi "qk"` only when the external definition was produced for QK's own ABI. C variadic declarations place a bare `...` at the end of their parameter list:

```qk
let printf(format: cstr, ...): i32 @foreign
```

The C ABI accepts only representations supported by the selected target. Multi-result functions, opaque values by value, nominal tagged unions, and other unsupported signatures are rejected.

## Exported symbols

`@export` gives a QK definition external default visibility. It defaults to the C ABI:

```qk
let add(left: i32, right: i32): i32
  @export(symbol "qk_add") = left + right
```

The short form `@export("qk_add")` sets only the symbol. `@export(abi "qk", symbol "...")` exports the definition with QK's ABI instead.

`pub` and `@export` serve different purposes. `pub` exposes a declaration to QK imports with hidden native visibility, while `@export` exposes an ABI symbol to the final binary or library.

## Link attributes

A module may describe its native link requirements with `@link`:

```qk
module graphics @link(
  system "m",
  search "vendor/lib",
  path "vendor/lib/libgraphics.a",
  framework "Cocoa",
)
```

`system` names a system library, `search` adds a library search directory, `path` links one explicit file, and `framework` selects an Apple framework. Link requirements are separate from import dependencies.

An `@link` body is a structured compile-time collection. `when` may select groups of complete link entries; an unselected group contributes nothing:

```qk
module graphics @link(
  when OS == .Linux {
    system "X11",
    path "vendor/linux/libgraphics.a",
  } else when OS == .Windows {
    system "gdi32",
    path "vendor/windows/graphics.lib",
  } else {
    @compiler_error("unsupported graphics target")
  }
)
```

Every branch must contain only complete `system`, `search`, `path`, or `framework` entries, nested link-item `when` groups, or `@compiler_error`. This preserves conditional native metadata without giving `when` general token-splicing behavior.

## Optimisation attributes

`@inline` requests inlining, and `@noinline` prevents it. `@noreturn` states that a function never returns to its caller:

```qk
let fail(message: str): void @noreturn {
  panic(message)
}
```

These attributes control inlining and return behavior. They do not replace correct source-level types.

# Packages and source layout

Each source file begins with a module declaration. Files in one selected package directory are merged, so declarations can refer to one another regardless of file discovery order.

For a non-command package beneath the project root, its module declaration is the complete dot-separated relative directory path. For example, `net/http/client.qk` declares:

```qk
module net.http
```

The command package name `main` is special. It may be declared in any selected directory, cannot be imported, and must provide `main()`.

Only immediate `.qk` files in each reachable package belong to that package. Unrelated source directories are not included automatically.

## Import resolution

An import path maps directly to a subdirectory of an ordered package root. The selected external library root is searched first, then the project root, repeated `-I` roots, and platform user and system roots.

When the selected package lies beneath the invocation directory, that invocation directory is the project root. Otherwise the selected package directory becomes its own root.

### Optional `qk.mod` source mounts

The nearest `qk.mod` in the selected package directory or one of its parents becomes the project root. The file is optional and does not declare a project-wide module name. It currently configures additional source locations:

```text
sources vendor "./vendor"
sources vendor "./more-vendor"
source vendor.mdhtml "./vendor/mdhtml/mdhtml"
```

Paths are relative to the directory containing `qk.mod` unless absolute.

`sources <prefix> <directory>` mounts a collection. Each first-level library beneath the directory keeps its own semantic name. With `sources vendor "./vendor"`, `import vendor.mdhtml` loads `./vendor/mdhtml`, whose files declare `module mdhtml`; `vendor/mdhtml/parser` declares `module mdhtml.parser`. Imports within that library continue to use those semantic names, such as `import mdhtml.parser`.

`source <prefix> <directory>` mounts one source tree at an exact public import prefix. With `source vendor.mdhtml "./vendor/mdhtml/mdhtml"`, `import vendor.mdhtml` loads that directory as semantic module `mdhtml`, and `import vendor.mdhtml.parser` loads its `parser` subdirectory as `mdhtml.parser`.

Directives are additive. Repeating `sources vendor` merges collection roots; an exact `source vendor.foo` can coexist with `sources vendor`; and project `vendor` collections merge with the `vendor` packages distributed in QK's installed library tree. If one visible import or one semantic module resolves to distinct source directories, compilation fails with an ambiguity or conflict diagnostic. Repeating an identical mapping is harmless.

The `std` and `std.*` names are reserved for sources beneath the `std` directory of the installed library root or a trusted `-stdlib` development override. Ordinary source packages cannot declare them.

QK distributions contain a top-level `libs` directory.
`go run ./cmd/qkc` discovers it from the QK checkout, while an installed
`<root>/bin/qkc` loads `<root>/libs`. `QK_LIB_DIR` overrides this lookup.
`-stdlib <dir>` selects the same kind of complete library root for a development build; only its `std` subtree is trusted.
Because the Go tool installs executables but not repository data, use
`scripts/dev_install.sh` rather than plain `go install` for a complete
development installation under `~/.local/share/qk`.

The standard library is an implicit dependency of ordinary modules unless `-nostdlib` is used.

## Explicit-file builds

Passing one `.qk` file to `build` or `run` creates a synthetic primary package from that file. This is convenient for small programs, while directory builds provide normal multi-file package behavior.

# Diagnostics and warnings

QK diagnostics report source ranges with line context. Installed library diagnostics use their source paths and retain the same source context.

QK warns about unreferenced local variables, loop bindings, and parameters. Prefixing a name with `_` does not suppress a warning; use the exact discard name `_` when no binding is wanted.

Warning categories currently include `unused-variable` and `unused-parameter`. The global policy is selected with:

```bash
qkc build -warn show .
qkc build -warn off .
qkc build -warn error .
```

Category overrides are order-independent:

```bash
qkc build \
  -warn error \
  -warn-unused-parameter off \
  .
```

`show` prints a warning, `off` discards it, and `error` promotes it to a compilation error.

# Building programs

`qkc build` accepts either a package directory or one `.qk` file. The package defaults to the current directory:

```bash
qkc build .
qkc build src/tool.qk
```

`-o` chooses the output path, and `-t exe|obj|lib|so|wasm` chooses an executable, combined relocatable object, static archive, shared library, or WebAssembly module. A `main` package defaults to an executable; a non-command package defaults to a static archive (`.a`, or `.lib` for MSVC). Final linking and archive creation use the target platform's tools from `PATH`.

`qkc run` builds an executable, launches it, and forwards every argument after the package argument:

```bash
qkc run ./cmd/tool input.txt --verbose
```

Program arguments are available as `std.os.args`, a `[]str`. The runtime owns the platform ABI entry point, fills this slice for hosted programs, runs module initialisers, and then calls QK's `main()`.

## Target and optimisation options

`-target <triple>` selects the effective target. `-sysroot`, `-cpu`, `-features`, `-target-abi`, `-relocation-model`, and `-code-model` refine native code generation and linking.

Optimisation is selected with `-O0`, `-O1`, `-O2`, `-O3`, `-Os`, `-Oz`, `-Ofast`, or `-Og`. `-release` independently changes the compile-time `ReleaseMode` value to `.Release`; it is not itself an optimisation level.

Use `-no-emit` to perform compilation checks without producing output. `-dump-ir`, `-dump-llvm`, and `-dump-asm` are development aids for inspecting compiler output.

## Libraries and freestanding builds

`-l`, `-L`, and `-Xlink` add native library, search-path, and linker arguments. `-static` requests static libraries where the target toolchain supports them.

`-nolibc` removes the hosted C runtime while retaining the QK standard-library portions that do not require it. Linux x86-64 freestanding executables use QK's own `_start`, exit by syscall, leave `std.os.args` empty, and provide the memory primitives LLVM may introduce.

Other freestanding executable targets are currently rejected until they have target-specific startup support. `-nostdlib` is separate: it omits QK's installed library sources altogether.

Cross-linking requires suitable CRT objects, libraries, and usually a sysroot for the selected target.

# Standard library tour

The installed standard library is divided into small `std` packages. Features that do not require libc remain available under `-nolibc`; hosted streams, files, and the libc allocator are selected out when libc is absent.

## Core methods

The root `std` package defines the common structural traits used by parameterized code, including arithmetic, comparison, and bitwise traits. It also provides trusted methods for numeric primitives, pointers, slices, `str`, and `cstr`.

Immutable and mutable slices provide search, prefix and suffix checks, filling, swapping, reversing, and allocator-backed reversed copies. Strings provide length-aware search, containment, trimming, reading, and hosted C-string conversion.

## Allocation

`std.alloc.Allocator` is the byte-oriented allocator trait. Typed helpers build on it:

```qk
let object, created = std.alloc.create<Point>(allocator)
defer std.alloc.destroy(allocator, object)

let values, allocated = std.alloc.allocate<i32>(allocator, 128)
defer std.alloc.free(allocator, values)
```

`create` and `destroy` work with pointers. `allocate`, `free`, and `resize` work with mutable slices so the element count travels with the allocation.

Hosted builds provide `std.alloc.LibcAllocator`. `std.alloc.Arena` is also hosted-only and obtains chunks directly from libc.

## Collections and strings

`std.collections.DynamicArray(T)` is an allocator-backed growable array. It supports reserving, appending, inserting, removing, clearing, slice views, and explicit `deinit`.

`std.strings.StringBuilder` is an allocator-backed builder that also implements reader, writer, and formatter traits. `std.strings` provides split iteration, replacement, and integer parsing in addition to the root string methods. Mutable `str` and `cstr` bindings are consuming `std.io.Reader` implementations.

## Input, output, and formatting

`std.io.Reader` and `std.io.Writer` are structural stream traits. Their default `read_to_builder` and `write_builder` methods transfer bytes to or from a `std.strings.StringBuilder`, so implementations only need to supply the core `read` or `write` method. `std.io.Format` lets a value format itself into a writer.

`std.io.wprint` and `wprintln` work with any writer. `print`, `println`, `eprint`, and `eprintln` use libc-free standard streams: Unix-family targets issue target-specific kernel calls with inline assembly, while Windows uses the stable Kernel32 console/file-handle API. Libc-backed file readers, file writers, and file printing are available when `std.io.HasFiles` is true.

Formatting uses `{}` for the next argument and `{N}` for an indexed argument. Floats use trimmed fixed-point output with six fractional digits by default, so values sufficiently close to zero print as `0`; `g` retains general/scientific notation. The other specifiers are `c`, `x`, and `X`. `{{` and `}}` emit literal braces.

```qk
std.io.println("name: {}, value: {1:x}", name, value)
```

Integer, floating-point, boolean, string, and C-string formatting is allocation-free. User types participate by supplying the structural `format` method.

## Files, paths, and operating-system values

Hosted `std.io.FileReader` and `FileWriter` provide checked open, read, write, flush, and close operations. Checked operations preserve `std.os.Error` values and partial byte counts; compatibility methods provide simpler boolean or count results.

`std.fs` supplies allocator-aware whole-file reads, writes, appends, sizing, removal, renaming, directory operations, and current-directory access. Its lexical path helpers handle platform separators without filesystem access.

`std.os.Error` wraps platform errors while reserving negative codes for library-originated failures. `std.os.args` holds command-line arguments for hosted executables, and `std.os.exit` terminates the process. Hosted programs can query environment variables with `std.os.environment_variable` (or `std.os.getenv`); the returned `str` is borrowed from the host environment. `std.os.environment_variable_owned` and `std.os.getenv_owned` copy the value into an allocator-backed `StringBuilder`.

## C library bindings

Hosted builds expose focused declarations in `std.libc`. Their platform-dependent C types and symbols are selected with `when`, making the bindings follow the effective target rather than the compiler host.

Prefer the higher-level allocation, stream, filesystem, and string packages when they fit. Use `std.libc` when direct C interoperability is the actual requirement.

## Mathematics

`std.math` provides `PI`, `TAU`, `E`, `PHI`, `SQRT2`, `LN2`, `LN10`, `LOG2E`, and `LOG10E`; floating-point classification, sign, minimum, maximum, decomposition, scaling, remainder, and rounding helpers; angle conversion; and trigonometric, hyperbolic, exponential, logarithmic, power, square-root, cube-root, and hypotenuse functions. `modf` returns the fractional part followed by the integral part, `frexp` returns a normalized fraction followed by its base-two exponent, and `sincos` returns sine followed by cosine. Hosted builds use the target's mathematics library where appropriate; `-nolibc` builds use QK implementations with explicit IEEE-754 special-value handling and no native library dependency.
