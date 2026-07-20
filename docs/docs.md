# The QK Language

QK is a small native systems language with C-like semantics, explicit data
layout, nominal user-defined types, pointers, slices, modules, methods, and
direct C interoperability. The language is under active development; this
document describes its current behavior rather than a stable standard.

## 1. Getting started

A source file uses the `.qk` extension and begins with a module declaration:

```qk
module main

let main() {
}
```

Compile every QK source file below a directory with:

```sh
qkc .
./main
```

The language has no garbage collector or ownership runtime. Memory allocation,
resource management, and operating-system interaction are provided through
libraries and foreign interfaces.

## 2. Installing the compiler

Build the compiler from the repository root:

```sh
go build ./cmd/qkc
```

During development, the equivalent direct invocation is:

```sh
go run ./cmd/qkc .
```

See the repository README for current build prerequisites. Linux, macOS, and
Windows hosts are supported. QK supports recognised 32-bit and 64-bit target
architectures. Hosted cross-compilation requires compatible target CRT objects
and native libraries.

The repository includes Vim file detection, syntax highlighting, indentation,
and file settings under `editor_support/vim`.

## 3. Lexical structure

### 3.1 Identifiers and keywords

Identifiers begin with an ASCII letter or `_`; subsequent characters may also be
ASCII digits. Identifiers are case-sensitive.

The reserved words are:

```text
alias alignof and as break continue defer else enum false for
given if import in len let module mut nil not offsetof opaque or pub
return sizeof struct trait true type union
```

Primitive type names such as `i32` and `void` are predefined identifiers. They
are not lexical keywords.

### 3.2 Whitespace and statement boundaries

Spaces, tabs, and carriage returns separate tokens. Newlines are significant as
statement and declaration terminators, although many delimited constructs permit
newlines between their parts. A semicolon can be used in place of a newline.

A backslash immediately followed by a newline suppresses that newline:

```qk
let value = left + \
    right
```

### 3.3 Comments

QK supports line and block comments:

```qk
// until the end of the line

/* across lines */
```

Block comments do not nest.

### 3.4 Numeric literals

Decimal integers and binary, octal, and hexadecimal integers are supported:

```qk
42
-42
0b101010
0o52
0x2a
```

Base prefixes are case-insensitive. Non-decimal floating-point literals, digit
separators, exponents, and numeric suffixes are not supported.

Decimal floating-point forms include:

```qk
1.25
1.
.25
-1.25
```

Numeric literals are initially untyped and acquire a concrete type from context.
An unconstrained numeric value cannot determine the type of a declaration or
function return.

### 3.5 Characters and strings

A character literal contains one byte:

```qk
'a'
'\n'
```

Ordinary string literals have fixed-size slice type `[char, N]`:

```qk
"hello"
```

A C string literal has type `cstr`, is NUL-terminated, and is written with a
leading `c`:

```qk
c"hello"
```

The supported escapes are `\\`, `\"`, `\n`, `\r`, `\t`, `\b`, `\f`, `\v`,
`\a`, and `\0`. Strings cannot span source lines. Raw strings, interpolation,
and Unicode escape syntax are not implemented.

### 3.6 Other literals

`true` and `false` have type `bool`. `nil` is a null pointer value whose initial
type is `*void`; an expected pointer type can specialize it.

## 4. Files, modules, and imports

### 4.1 Module declarations

Every source file currently requires a module declaration as its first top-level
statement:

```qk
module graphics
```

All files declaring the same module contribute to one module scope. Module names
are dot-separated identifier paths and do not need to match directory names:

```qk
module graphics.formats.png
```

The dotted path is a canonical module name, not a visibility relationship.
Parent, child, and sibling modules do not receive implicit access to one another.

The compiler starts from a root module and processes that module and its transitive
imports. The default root module is `main`; select another with `-m`:

```sh
qkc -m graphics .
```

The root module is a build concept. It is not necessarily a module named `main`.

### 4.2 Imports

Import a module with:

```qk
import math
```

An optional second identifier is the local alias:

```qk
import graphics.formats.png png
```

Several modules can be imported together:

```qk
import (
    math,
    libc c,
)
```

Access imported names by chaining `.` through the canonical module path, or
through an explicit alias:

```qk
let angle: f64 = math.atan2(y, x)
let file: *c.FILE
```

For example, `import std.optional` makes `std.optional.foo` available. Importing
a child does not import its parent as a module; intermediate path components are
namespaces used to reach the imported module. Explicit aliases replace the full
path locally. Imports with overlapping prefixes may coexist, while aliases and
top-level names must remain unique.

Imports establish dependency order and make a module name or alias visible. They
do not textually include a file. Unknown modules, unknown qualified symbols, and
circular imports are errors.

### 4.3 Visibility

Top-level functions, values, and types are private to their module unless declared
`pub`:

```qk
pub let answer: i32 = 42
pub let parse(input: str): Result = { /* ... */ }
pub let Handle = type opaque
```

`pub` controls source-level access only. It does not export a native linker symbol;
use `@export` for that.

All files in one module share a scope, so declarations in one file can be used by
another without an import. A name may be defined only once in a scope.

### 4.4 Source discovery

The compiler recursively discovers `.qk` files in the base directory supplied on
the command line and the platform's QK data directories. On Unix-like hosts these
are:

- `~/.local/share/qk`
- `/usr/local/lib/qk`
- `/usr/lib/qk`

On Windows they are `%AppData%\qk` and, when available, `%ProgramData%\qk`.

Use repeated `-E path` options to exclude files or directory trees. Only modules
reachable from the selected root module contribute to the output.

## 5. Top-level structure

Apart from the initial module declaration, top-level forms are imports, functions,
global values, and type declarations. `pub` may prefix functions, globals, and
types. Declarations are terminated by a newline or semicolon.

Functions and types can refer to later top-level declarations. Local declarations
remain lexically scoped.

A type, function, global, or imported module alias cannot reuse another such name
in the same scope. Method names are unique within their owner type.

### 5.1 Compile-time `when`

`when` conditionally includes source. Conditions support boolean literals, `not`,
`and`, `or`, equality, and build target and configuration values:

```qk
when NoLibc {
    // Freestanding path.
} else {
    // libc-backed path.
}
```

| Value | True when |
| --- | --- |
| `NoLibc` | The compiler was invoked with `-nolibc`. |
| `NoStdlib` | The compiler was invoked with `-nostdlib`. |

These booleans can be combined with `OS`, `Arch`, `Environment`, and
`PointerBits`:

```qk
when NoLibc and OS == .Linux and Arch == .X86_64 {
    // Linux x86-64 freestanding code.
}
```

`NoStdlib` describes whether the QK `std` module is loaded; it is independent of
whether the platform C library is linked.

Trusted standard-library sources can declare boolean capabilities:

```qk
module std

let HasPrint = compile_time not NoLibc
let HasFormatting = compile_time HasPrint
```

Capability declarations are compile-time values, not runtime globals or members
of the `std` namespace. Their names are globally available only inside `when`
conditions:

```qk
when HasPrint {
    import std
    std.println("available")
}
```

Only the standard library or an explicitly trusted `-stdlib` source tree may
declare capabilities. Names must begin with `Has`, initializers must be boolean
compile-time expressions, and declarations must be top-level. They may reference
target/configuration values and other capabilities. Forward references are
supported; duplicate declarations, unknown references, and dependency cycles are
errors.

With `-nostdlib`, known standard-library capability names remain available but
evaluate to `false`. This permits project sources to guard imports
without producing unknown-name errors, while misspelled or otherwise unknown
capability names remain diagnostics. An external `-stdlib` tree supplies its own
capability set while selected.

## 6. Declarations and scope

### 6.1 Values

`let` introduces an immutable value:

```qk
let count: u32 = 10
let name = get_name()
```

Add `mut` when the binding can be assigned:

```qk
let mut count: u32 = 10
count += 1
```

The annotation may be omitted when the initializer already has a concrete type.
Untyped numeric literals and unresolved enum shorthand require an annotation or
explicit cast:

```qk
let count: i32 = 10
let mode: Mode = .read
```

A declaration normally needs an initializer. An attributed foreign declaration
may omit it but must state its type.

Declarations may appear at module or block scope. Nested blocks introduce nested
lexical scopes. A declaration can shadow a name from an outer scope but cannot
duplicate a name in the same scope.

Global initializers must be native constants. Currently accepted forms are numeric,
boolean, character, C-string, null, and enum literals, plus struct literals built
recursively from those forms. Function calls, ordinary QK string/slice literals,
and general computed expressions cannot initialize globals; move such work into a
function.

### 6.2 Assignment

Assignment requires a mutable place. Valid targets include mutable bindings,
fields and slice elements rooted in mutable bindings, and values reached through
mutable pointers:

```qk
value = replacement
point.x = 3
items[0] = item
pointer.* = item
```

Compound assignments are available for arithmetic, bitwise operations, and shifts:

```text
+=  -=  *=  /=  %=  &=  |=  ^=  <<=  >>=
```

QK deliberately has no increment or decrement operator; use `+= 1` or `-= 1`.

### 6.3 Type declarations

A nominal type is declared with `type`:

```qk
let UserId = type u64
let Point = type struct { x: f64, y: f64 }
```

A transparent alias adds `alias`:

```qk
let Byte = type alias u8
```

Nominal types are distinct from their underlying types. Transparent aliases are
interchangeable with their targets. Type declarations can be `pub`.

## 7. Functions

### 7.1 Definitions and returns

A function can have a block body:

```qk
let add(a: i32, b: i32): i32 {
    return a + b
}
```

or an expression body:

```qk
let add(a: i32, b: i32): i32 = a + b
```

The return type can be inferred from the expression body or from all `return`
statements in a block body. A function with no value returns `void`. Inference
requires a concrete and consistent type; annotate returns based only on untyped
numeric literals:

```qk
let one(): i32 = 1
```

Numeric return branches use the normal numeric promotion rules. A foreign function
must always state its return type, including `void`.

### 7.2 Parameters

Each parameter has a name and type. Adjacent names can share a trailing type:

```qk
let sum(a, b, c: i32): i32 = a + b + c
```

`mut` makes a parameter binding assignable inside the function:

```qk
let consume(mut remaining: usz) { /* ... */ }
```

It does not make a pointed-to value mutable; that is part of the pointer type.

### 7.3 Default parameters

Trailing parameters may have defaults:

```qk
let open(path: str, mode: Mode = .read, retries: u32 = 0) { /* ... */ }
```

Defaults can precede a shared type annotation:

```qk
let offset(x = 0, y = 0: i32): Point = Point { x = x, y = y }
```

A parameter without a default cannot follow one with a default. Default expressions
are resolved in the declaring function's scope and are evaluated when omitted by a
call. Defaults do not alter the function's full type or ABI. They are unavailable
on functions using the C ABI.

### 7.4 Variadic functions

Bare `...` declares an untyped C variadic tail:

```qk
let printf(format: cstr, ...): i32 @foreign
```

Bare variadic functions must use the C ABI. Fixed arguments are checked normally.
Extra arguments must already have concrete types, so cast otherwise-unconstrained
numeric literals before passing them.

A named typed variadic parameter uses `name: ...Type` and is available as a
dynamic slice inside a QK function:

```qk
let sum(values: ...i64): i64 {
    let mut result: i64 = 0
    for value in values { result += value }
    return result
}

let inspect(values: ...dyn Any) { /* values has type [dyn Any] */ }
```

Calls accept zero or more separately checked arguments. Inside the function, the
arguments are available as one dynamic slice:

```qk
sum()
sum(10, 20, 30)
inspect(number.&, file.&)
```

An existing slice can supply the complete variadic tail with postfix `...`:

```qk
let numbers: [i64, 3] = [10, 20, 30]
sum(numbers...)
```

Typed variadics are QK-ABI-only. They must be final, cannot use defaults, and
cannot appear on C foreign or exported functions. Bare C variadics and typed QK
variadics are distinct features.

### 7.5 Calls and function values

Call a function or callable pointer with parentheses:

```qk
let result = transform(input)
callback(context)
```

Calls check required and maximum argument counts and apply permitted implicit
conversions. A function name used as a value has pointer-to-function type.

Function-pointer syntax is:

```qk
let Callback = type alias *(i32, *void): bool
```

Function types contain parameter and return types. A typed variadic function
pointer is written as `*(...Type): Return`. Defaults, parameter names, bare C
variadic status, and foreign/export properties are not part of a function-pointer
type.

### 7.6 Program entry point

Executable output requires `main` in the selected root module:

```qk
let main() {
}
```

`main` must have no parameters, must have a body, must return `void`, and cannot
carry attributes.

## 8. Methods

Methods are declared by qualifying a function name with a local nominal type:

```qk
let Point.length(self): f64 = math.sqrt(self.x * self.x + self.y * self.y)
```

The first parameter selects the receiver form:

```qk
let Value.inspect(self) { /* value receiver */ }
let Value.read(*self) { /* immutable pointer receiver */ }
let Value.update(*mut self) { /* mutable pointer receiver */ }
```

Call them through a value or pointer:

```qk
let length = point.length()
point.update()
```

Calls apply the supported address/dereference adaptation for the receiver,
subject to mutability rules.

Omitting `self` declares a static method:

```qk
let Point.origin(): Point = Point { x = 0.0, y = 0.0 }
let origin = Point.origin()
```

The owner type must be a nominal type declared in the current module. Methods
cannot be attached to imported types or transparent aliases. A public method
requires a public owner. Method names must be unique for the owner and cannot
collide with a field, including a promoted anonymous-union field.

Methods otherwise support ordinary parameters, defaults, inferred returns, and
function attributes.

## 9. Statements and blocks

A block is a sequence of statements in braces and introduces a lexical scope:

```qk
{
    let value = acquire()
    use(value)
}
```

Statements end at a newline or semicolon. Calls are valid expression statements;
arbitrary unused expressions are not.

`return` exits the current function, optionally with a value. `break` exits the
nearest loop and `continue` starts its next iteration. `break` and `continue` are
invalid outside a loop. Every reachable exit from a non-`void` block-bodied
function must return a value.

### 9.1 Deferred actions

`defer` schedules a call/expression or block for the end of the current scope:

```qk
let file = open_file(path)
defer close_file(file)

defer {
    release(first)
    release(second)
}
```

Deferred actions run in reverse registration order. They also run when the scope
is left through `return`, `break`, or `continue`.

## 10. Conditionals

Statement conditionals require `bool` conditions and do not use parentheses:

```qk
if ready {
    start()
} else if retryable {
    retry()
} else {
    fail()
}
```

An `if` can also produce a value. Its branches contain one expression:

```qk
let sign: i32 = if value < 0 { -1 } else if value > 0 { 1 } else { 0 }
```

Value branches must agree on a type, with numeric promotion applied where
possible. An expected outer type is propagated into every branch. In value-using
code, provide an `else` so every path yields a value.

## 11. Loops and iteration

`for` provides all loop forms. An empty header is an infinite loop:

```qk
for {
    service_one_request()
}
```

A single expression is a `bool` condition:

```qk
for remaining > 0 {
    remaining -= 1
}
```

The three-part form separates initializer, condition, and post statement with
semicolons:

```qk
for let mut i: usz = 0; i < len(items); i += 1 {
    use(items[i])
}
```

Iterate over an integer range with `..` or inclusive `..=`:

```qk
for i in 0..count { /* 0 through count - 1 */ }
for i in 0..=last { /* 0 through last */ }
```

Range bounds must be integers and are converted to `usz`; the iterator is an
immutable `usz` binding.

Iterating a slice binds each element value:

```qk
for item in items {
    consume(item)
}
```

Both fixed-size and dynamic slices are iterable. `break` and `continue` target the
innermost loop.

## 12. Expressions and operators

### 12.1 Precedence

Operators are listed from lowest to highest precedence. Operators on the same row
associate left-to-right unless described as prefix or postfix.

| Precedence | Operators |
| --- | --- |
| 1 | `or` |
| 2 | `and` |
| 3 | prefix `not` |
| 4 | `|` |
| 5 | `^` |
| 6 | `&` |
| 7 | `==`, `!=`, `<`, `<=`, `>`, `>=` |
| 8 | `<<`, `>>` |
| 9 | `+`, `-` |
| 10 | `*`, `/`, `%` |
| 11 | prefix `-`, `~` |
| 12 | calls, indexing, field/method access, postfix pointer operations and casts |

Parentheses override precedence. `and` and `or` short-circuit and require `bool`
operands; `not` is prefix logical negation.

### 12.2 Arithmetic and comparison

`+`, `-`, `*`, `/`, and `%` require numeric operands except for the pointer
arithmetic forms described below. Operands are promoted to a common type.

`&`, `|`, `^`, `<<`, `>>`, and `~` require integers. Equality accepts mutually
compatible types. Ordering comparisons require numeric operands. Every comparison
produces `bool`.

### 12.3 Postfix expressions

Postfix operations can be chained:

```qk
factory().items[index].process()
```

Supported postfix forms are:

- `callee(args...)` — call.
- `value[index]` — slice or pointer indexing.
- `value.field` — field or method selection.
- `value.*` — pointer dereference.
- `value.&` — immutable reference.
- `value.&mut` — mutable reference.
- `value.(Type)` — explicit cast.

`as` is reserved but is not the current cast syntax.

### 12.4 Pointer arithmetic

For a pointer `p` and integer `n`, `p + n`, `n + p`, and `p - n` move by `n`
elements. Subtracting pointers with identical base types yields an `isz` element
distance. Arithmetic is forbidden on pointers to `void`, functions, or incomplete
types.

### 12.5 Compile-time layout operations

`sizeof` accepts a type or expression and produces its size as `usz`:

```qk
sizeof(Header)
sizeof(value)
```

`alignof` similarly produces alignment as `usz`. Expression operands of `sizeof`
and `alignof` are not evaluated. Both require complete object types; `void` and
function types have no object alignment.

`offsetof(Type, field)` produces a field offset as `usz` for structs and unions,
including promoted fields of anonymous unions.

`len(slice)` returns a slice's length as `usz`.

### 12.6 `given` expressions

`given` evaluates statements in a nested scope and then yields a final expression:

```qk
let result = given {
    let intermediate = prepare(input)
    defer release(intermediate)
} -> finish(intermediate)
```

Names declared in the block are visible to the final expression. The final
expression determines the value and type, with an expected outer type propagated
into it.

## 13. Literal and aggregate expressions

### 13.1 Struct literals

A named struct value lists fields with `=`:

```qk
let point = Point {
    x = 1.0
    y = 2.0
}
```

Commas or newlines separate fields. Imported types can be qualified. Context can
supply the type, allowing an anonymous-looking literal:

```qk
let point: Point = { x = 1.0, y = 2.0 }
```

Without an expected named type, `{ field = value }` creates an anonymous structural
value. Named literals reject unknown and duplicate fields and require every
ordinary struct field. A union literal initializes exactly one field.

### 13.2 Enum literals

Use a qualified variant through its type or use leading-dot shorthand when context
identifies the enum:

```qk
let mode = Mode.read
let other: Mode = .write
```

Leading-dot shorthand is also resolved from the opposite operand in a comparison
and from parameter, return, assignment, field, or aggregate context.

### 13.3 Slice literals

List elements directly:

```qk
let values: [i32, 3] = [1, 2, 3]
```

or repeat one value:

```qk
let zeroes: [u8, 4096] = [0; 4096]
```

An unconstrained non-empty literal chooses a common element type and has fixed
size equal to its element count. Empty literals need an expected slice type. The
repeat amount is converted to `usz`; a literal repeat count contributes a known
fixed size.

## 14. Primitive types

| Category | Types |
| --- | --- |
| Signed integers | `i8`, `i16`, `i32`, `i64`, `isz` |
| Unsigned integers | `u8`, `u16`, `u32`, `u64`, `usz` |
| Floating point | `f32`, `f64` |
| Other | `bool`, `char`, `void` |

`isz` and `usz` are pointer-sized signed and unsigned integers. They are 32 bits
on 32-bit targets and 64 bits on 64-bit targets.

`char` is a distinct byte-sized character type, not an alias of `u8`. `bool` is a
distinct logical type. `void` denotes no returned value or an untyped pointee; it
cannot be stored by value.

The predefined aliases are:

```qk
str  // [char]
cstr // *char
```

`str` is length-delimited and does not imply a NUL terminator. `cstr` points to a
NUL-terminated character sequence.

## 15. Numeric types and conversion

Integer literals begin as untyped integers and floating literals as untyped
floats. Context supplies a concrete type in declarations, arguments, returns,
aggregate elements, assignments, and explicit casts.

Implicit numeric conversions are intentionally narrow:

- Signed integers widen only to equal-or-wider signed integers.
- Unsigned integers widen only to equal-or-wider unsigned integers.
- Signed and unsigned typed integers do not implicitly mix.
- Integers convert implicitly to floating point.
- `f32` widens to `f64`.
- Floating-point values do not implicitly convert to integers.

When two operands differ, arithmetic and bitwise operations choose the wider type
within a compatible family. A typed integer combined with an untyped floating
literal promotes to `f64`; an untyped integer adopts the other numeric operand's
type.

Use postfix casts for narrowing, signedness changes, float/integer conversion,
integer/`bool`, integer/`char`, or integer/pointer conversion:

```qk
let narrow = wide.(u16)
let address = raw.( *mut Packet )
```

Spaces in type syntax are optional; the spaced pointer form above is only for
readability. Explicit conversion does not add range or overflow checks.

## 16. Pointers, references, and mutability

`*T` is a pointer through which `T` cannot be modified. `*mut T` permits pointee
mutation:

```qk
let input: *Buffer
let output: *mut Buffer
```

A mutable pointer converts implicitly to its immutable counterpart, not vice
versa. Pointers involving `void` can be converted for interoperability. Explicit
casts support pointer-to-pointer and pointer/integer conversion, but cannot use a
cast to manufacture mutability from an immutable pointer.

References use postfix syntax and require addressable places:

```qk
let pointer = value.&
let mutable_pointer = value.&mut
```

A mutable reference requires a mutable binding or a place already reached through
mutable access. Fields and indices inherit addressability and mutability from their
base. Dereference with `pointer.*`; a `*void` cannot be dereferenced or indexed.

Pointers can be indexed like arrays. Pointer indexing and arithmetic require a
complete, non-`void`, non-function base type.

QK pointers are unmanaged native addresses. The language performs no lifetime,
aliasing, ownership, or null-safety analysis.

## 17. Slices

A slice type is either dynamic or fixed-size:

```qk
[T]     // dynamic length
[T, N]  // fixed length
```

Fixed-size slices contain their elements inline. Dynamic slices carry a data
pointer and length. Their length is available through `len`.

A fixed slice can be used where a compatible dynamic slice is expected. Fixed
slices of different known sizes are not interchangeable. Element types follow
their normal coercion rules.

Slices can convert to compatible element pointers. Character slices are an
exception: `str` is not implicitly a `cstr`, because it is length-delimited and
need not be NUL-terminated. Crossing that boundary requires an explicit operation
or cast and a valid terminator supplied by the program.

Indexing accepts an integer. The compiler diagnoses a literal index outside a
known fixed size. General runtime indexing is not bounds checked; an invalid index
has native undefined behavior.

Mutation through a slice index depends on the mutability of the place holding the
slice. Mutation through a pointer index depends on pointer mutability.

Slice element types must be complete. Literals, repetition, and iteration are
described in Sections 11 and 13.

## 18. Structs

An anonymous struct type declares ordered named fields:

```qk
struct {
    tag: u32
    payload: [u8, 16]
}
```

Structs may be empty. Field order is layout order; target ABI rules determine
alignment and padding. Anonymous struct identity is structural: field names,
order, and types must match.

Most structs are given nominal identity:

```qk
pub let Header = type struct {
    tag: u32
    payload: [u8, 16]
}
```

Access a field with `value.field`. Field access through a pointer is automatic.
Literals use `field = expression` as described in Section 13.

A struct cannot contain an incomplete type by value. Recursive structures must
cross pointer indirection:

```qk
let Node = type struct {
    next: *Node
    value: i32
}
```

## 19. Unions

A union overlays non-empty named fields:

```qk
let Value = type union {
    integer: i64
    real: f64
    pointer: *void
}
```

Its size is sufficient for its largest field and its alignment is the maximum
field alignment. Named unions are nominal types. QK does not track an active
field; reading a different field from the one most recently written exposes the
underlying representation.

A union literal initializes exactly one field:

```qk
let value: Value = { integer = 42 }
```

An unnamed union can be embedded directly in a struct:

```qk
let Token = type struct {
    kind: TokenKind
    union {
        integer: i64
        text: str
    }
}
```

Embedded-union fields are promoted for access, literals, method-name collision
checks, and `offsetof`:

```qk
let integer = token.integer
```

Union fields must be unique and complete by value.

## 20. Enums

An enum contains at least one variant. Without explicit values, variants are
numbered from zero:

```qk
let Colour = type enum {
    red,
    green,
    blue,
}
```

Explicit integer values are also supported:

```qk
let Status = type enum {
    ok = 200,
    not_found = 404,
    failure = 500,
}
```

One enum cannot mix implicit and explicit values. Names must be unique. Values
must fit the compiler's accepted 32-bit enum range. The representation is a
32-bit integer, but enums are nominal and do not implicitly interchange with
integers or other enum types.

Use `Type.variant` when the type must be explicit, or `.variant` where context
already supplies it. Enum values support equality with the same enum type.

## 21. Defined types, aliases, opaque types, and recursion

`type` creates a nominal type:

```qk
let FileDescriptor = type i32
```

`FileDescriptor` has the same representation as `i32` but is not implicitly
interchangeable with it. Convert explicitly when crossing the boundary:

```qk
let raw: i32 = descriptor.(i32)
let descriptor = raw.(FileDescriptor)
```

`type alias` creates a transparent spelling:

```qk
let Size = type alias usz
```

Aliases do not introduce nominal identity and cannot own methods.

Recursive type cycles are allowed only behind pointers. Direct cycles and cycles
through by-value aggregate fields are rejected because their layout is infinite.

`opaque` defines an incomplete type for handles whose representation is unknown:

```qk
pub let FILE = type opaque
let fopen(path: cstr, mode: cstr): *FILE @foreign
```

Opaque types must be nominal; a transparent opaque alias is invalid. They can be
named, referenced, and passed behind pointers, but cannot be stored, passed, or
returned by value. They also cannot appear by value inside structs, unions, or
slices, participate in pointer arithmetic, or be used with `sizeof`/`alignof`.

## 22. Function types and callable values

A function pointer begins with `*`, followed by parenthesized parameter types and
a return type:

```qk
*(i32, i32): i32
*(): void
```

Function names can be assigned or passed where a compatible function pointer is
expected:

```qk
let BinaryOp = type alias *(i32, i32): i32
let operation: BinaryOp = add
let result = operation(2, 3)
```

Function type equality requires equal parameter and return types. Function
coercion uses contravariant parameter compatibility and covariant return
compatibility. Function pointers cannot be declared `*mut`; mutability applies to
data reached through a pointer, not to code.

The callable type does not encode symbol linkage, C/QK ABI selection, defaults,
or bare C variadic behavior. Preserve those properties by calling a declared
function directly when they matter. Typed QK variadic behavior is retained by
the callable type because it uses an ordinary fixed slice ABI.

## 23. Scopes and name resolution

Names are resolved through nested scopes in this order:

1. The innermost block or expression scope.
2. Enclosing blocks and the current function's parameters.
3. The current module.
4. Predefined types.

Imports bind a module or its alias in module scope. Qualified access resolves a
public symbol in that module. Unqualified imported members are not injected into
the current scope.

Function parameters are visible in the body. Default expressions are resolved in
the declaring function's scope. Loop iterators are visible only in the loop body.
Names declared inside a `given` block remain visible to its final expression but
not outside the `given` expression.

Top-level types and function signatures are collected before bodies are resolved,
allowing forward references and mutually recursive functions. Local values follow
normal lexical declaration order.

A symbol cannot be redefined in one scope. Inner scopes may shadow outer names.
Module visibility is enforced after qualification: private names remain usable
throughout their defining module but not by importers.

## 24. Attributes

Attributes follow the declaration they modify and begin with `@`. Multiple
attributes may be placed consecutively or on following lines.

### 24.1 Optimization and control-flow attributes

Functions accept:

```qk
@inline
@noinline
@noreturn
```

Empty parentheses are also accepted. `@inline` requests forced inlining,
`@noinline` prohibits it, and `@noreturn` states that the function never returns
to its caller. Violating `@noreturn` is erroneous program behavior; the promise
is not checked at runtime.

Attributes are not currently deduplicated. Repeating an attribute is unsupported;
write each applicable attribute at most once.

### 24.2 Foreign declarations

`@foreign` declares a function or global provided by another native object:

```qk
let puts(text: cstr): i32 @foreign
let errno: i32 @foreign
```

The default external symbol is the QK declaration name and the default ABI is C.
Named options override either property:

```qk
let allocate(size: usz): *mut void
    @foreign(symbol "malloc", abi "c")
```

Supported ABI names are `"c"` and `"qk"`. A foreign function has no QK body and
must state a return type. A foreign global without an initializer must state its
value type.

### 24.3 Exported definitions

`@export` gives a QK function an externally visible native symbol. It defaults to
the C ABI and the QK function name:

```qk
let add(a, b: i32): i32 @export = a + b
```

Rename the native symbol with the short form:

```qk
let initialise() @export("library_init") { /* ... */ }
```

or named options:

```qk
let initialise()
    @export(symbol "library_init", abi "qk")
{
    /* ... */
}
```

A function cannot be both `@foreign` and `@export`. C ABI exported functions
cannot use default parameters.

### 24.4 Link attributes

A module declaration can attach native link requirements:

```qk
module image
    @link(
        system "png",
        search "vendor/lib",
        path "vendor/lib/helper.o",
        framework "CoreGraphics",
    )
```

Entries mean:

| Entry | Linker effect |
| --- | --- |
| `system "name"` | Link `-lname`. |
| `path "file"` | Pass a file directly to the linker. |
| `search "directory"` | Add `-Ldirectory`. |
| `framework "name"` | Link an Apple framework. |

`@link` requires at least one non-empty entry. Relative `path` and `search`
values are resolved relative to the source file containing the module declaration.
Link requirements from every reachable module are accumulated for the final link.

## 25. ABI, linkage, and C interoperability

QK distinguishes source visibility, native linkage, and calling convention:

- `pub` permits access from another QK module.
- `@export` creates an externally visible native definition.
- `@foreign` refers to an externally provided native declaration.
- `abi "c"` or `abi "qk"` selects the calling convention where supported.

Ordinary QK functions and globals, including `pub` declarations, are not part of
the public native interface. Use `@export` for definitions that native callers
must access.

Both `@foreign` and `@export` default to the C ABI. Supported aggregate calling
conventions depend on the selected target. Some target/aggregate combinations
remain unsupported.

Typical C interoperation uses `cstr`, opaque pointer types, C-layout aggregates,
foreign functions/globals, and module link attributes:

```qk
module files @link(system "c")

pub let FILE = type opaque

let fopen(path: cstr, mode: cstr): *FILE @foreign
let fclose(file: *FILE): i32 @foreign
```

QK structs and unions use the selected target's native layout, but matching a
particular C declaration remains the programmer's responsibility. Check target
widths, signedness, packing expectations, and library headers. QK has no
packed-struct or bit-field syntax.

The QK ABI is intended for calls entirely controlled by QK and is not a stable C
interface. Function-pointer types do not distinguish C and QK calling conventions,
so indirect foreign calls should be avoided unless the signature is known to be
ABI-compatible.

## 26. Runtime semantics

QK values use native value semantics. Primitive values, pointers, enums, fixed
slices, structs, and unions are copied on assignment and argument passing.
Copying a dynamic slice does not copy its referenced elements.

Local storage has lexical lifetime. Global values have program lifetime. Pointers
do not extend either lifetime. Returning or retaining an address to expired local
storage is invalid.

`and` and `or` evaluate left-to-right and short-circuit. Deferred actions execute
in reverse registration order when their scope exits. The language does not yet
specify a general evaluation-order guarantee for all other subexpressions; avoid
depending on relative side effects between operands or arguments.

QK deliberately exposes native unsafe behavior. The following are not generally
checked at runtime:

- Integer overflow and narrowing overflow.
- Division or remainder by zero.
- Invalid shift counts.
- Null, dangling, or misaligned pointer access.
- Out-of-bounds slice or pointer indexing.
- Reading a union through an inactive/incompatible field.
- Invalid values manufactured by casts.

Such operations have native-machine behavior and may be undefined. Some cases,
such as literal indexing outside a fixed-size slice, are rejected during the
build.

There is no allocator, exception mechanism, stack unwinding, garbage collector,
or automatic destructor system. `defer` does not provide exception unwinding.

### 26.1 Traits and dynamic dispatch

A trait is a nominal, unsized set of method requirements:

```qk
pub let Reader = type trait {
    let name(self): str
    let position(*self): usz
    let read(*mut self, buffer: [mut u8]): usz
}
```

`self`, `*self`, and `*mut self` are supported and must match the concrete
method's receiver exactly. A concrete nominal type conforms structurally when
its accessible method set contains exact matches for every requirement. No
conformance declaration is written. Dynamic calls to a value receiver operate on
a copy; they do not move or mutate the original value.

Traits cannot be used by value. `dyn Trait` and `mut dyn Trait` create immutable
and mutable dynamic trait types that retain the concrete value's type identity.
Immutable trait pointers can call only `*self` methods; mutable trait pointers can
call both receiver forms.

`Any` is a built-in empty trait implemented by every concrete type. Trait
pointers support trapping assertions. A pointer assertion aliases the original
storage, while a value assertion copies the concrete value:

```qk
let erased: dyn Any = file.&
let pointer = erased.(*File)
let copy = erased.(File)
```

Assertions check nominal runtime type identity. A mismatch calls `panic` with a
diagnostic. Trait pointers are QK-ABI-only and cannot cross a C ABI boundary.

When an addressable concrete value is used where a trait pointer is expected,
QK inserts the reference and structural conversion implicitly:

```qk
let reader: dyn Reader = file       // file.&.(dyn Reader)
consume_reader(file)                // parameter type is dyn Reader
```

An expected `mut dyn Trait` similarly inserts `.&mut.(mut dyn Trait)`, but only for a
mutable place. Immutable trait pointers also accept computed concrete expressions.
Untyped numeric literals still need a cast because the trait alone cannot infer
their concrete numeric type. Implicit conversion never weakens mutability rules.

Use a two-target checked assertion to inspect a trait pointer without trapping:

```qk
let file, ok = erased.(*File)
```

The first target receives the asserted pointer or copied value on success and
the zero value on failure. The second target receives `true` only when the
runtime concrete type matches exactly.

The same form performs a checked trait-to-trait recast:

```qk
let printable, ok = erased.(dyn Printable)
```

When a cast is statically certain, the syntax remains accepted and the second
result is the constant `true`:

```qk
let printable, ok = value.&.(dyn Printable)
let widened, ok = number.(i64)
```

Checked assertions and recasts are available only as the right-hand side of a
two-target declaration or assignment. Single-target casts retain their existing
trapping behavior.

### 26.2 Display

The standard library defines `std.Display` with one value-receiver method:

```qk
pub let Display = type trait {
    let display(self): void
}
```

All value-bearing builtin types implement it: signed and unsigned integers,
pointer-sized integers, `f32`, `f64`, `char`, `bool`, `str`, and `cstr`.
`display()` writes the value to standard output without a trailing newline.
Numeric output uses the corresponding C formatting, booleans are `true` or
`false`, characters are written directly, and `str` output respects its explicit
length rather than requiring NUL termination. `void` has no values and therefore
cannot implement a value-receiver trait.

Methods are available through the implicit standard-library dependency. Naming
the trait itself requires its module qualification:

```qk
let count: i32 = 42
count.display()

let shown: *std.Display = count.&.(*std.Display)
shown.display()
```

Builtin `Display` methods are unavailable when `NoLibc` is true.

The standard library also provides typed, type-safe formatting through
`std.print`:

```qk
std.print("hello {} {1}", foo, bar)
```

Its signature is `print(format: str, arguments: ...dyn Any): void`. Each field
advances the automatic argument position once. `{}` selects the current automatic
argument, while a zero-based indexed field such as `{1}` overrides the selection
for that field without changing how the automatic position advances. Thus
`{1} {}` selects argument 1 twice. Indexed fields allow arguments to be reordered
or reused. If the selected argument's runtime concrete
type implements `std.Display`, `print` dynamically calls its `display()` method;
otherwise it writes `<?>`. A missing or out-of-range argument also writes `<?>`,
and extra arguments are ignored. `{{` and `}}` emit literal braces; malformed
fields are emitted literally. Formatting itself adds no newline. `std.println`
has the same formatting behavior and appends one newline.

Arguments rely on implicit concrete-to-trait borrowing. Both addressable and
computed values are accepted. A private `display` method can satisfy the trait:
visibility still prevents another module from naming the method directly, but
does not prevent invocation through the trait. Converting an existing trait
pointer to a different trait pointer traps if the concrete type does not conform;
the two-target form reports failure through its boolean result instead.

### 26.3 Panic

`panic(message: str)` writes `panic: `, the message, and a newline to standard
error, then terminates without stack unwinding. Deferred actions are not run as
part of panic termination. Failed trait assertions use the same message prefix.

## 27. Builds and output

`qkc` discovers source files, selects the root module and its transitive imports,
checks the complete program, and produces the requested executable, shared
library, relocatable object, or assembly output. Unreferenced definitions are not
guaranteed to remain in native output.

## 28. Compiler command line

The general invocation is:

```text
qkc [options] <baseDir>
```

Exactly one base directory is required. Options may appear before or after it.

### 28.1 Inputs and root module

| Option | Meaning |
| --- | --- |
| `-m module` | Select the root module; default `main`. |
| `-E path` | Exclude a source file or directory tree; repeatable. |
| `-stdlib path` | Trust and use a replacement standard-library source tree. |
| `-nostdlib` | Disable standard-library loading. |
| `-no-emit` | Check the program without writing a final file. |

### 28.2 Standard library

The `std` module is available by default, and its public methods on builtin types
can be called without importing the module name. The `std` and `std.*` namespaces
are reserved. Use `-nostdlib` to build without the standard library.

The `std.libc` submodule provides C declarations when `HasLibc` is true. Import
it explicitly before use:

```qk
import std.libc

let memory = std.libc.malloc(1024)
```

In a `-nolibc` build the module remains available, but its C declarations are not.

`-stdlib` selects a trusted replacement standard-library source tree:

```text
go run ./cmd/qkc . -stdlib /path/to/qk-stdlib
```

Every file in that tree must declare `module std`. The path is trusted to define
reserved modules and methods on builtin types, so do not use a tree controlled by
an untrusted project.

### 28.3 Output selection

| Option | Meaning |
| --- | --- |
| `-o file` | Set the output path. |
| `-t type` | Select `exe`, `obj`, or `so`. |
| `-run` | Run an executable after a successful build. |

Accepted output-type aliases are:

- Executable: `exe`, `executable`, `.exe`.
- Relocatable object: `obj`, `object`, `.o`.
- Shared library: `so`, `shared`, `sharedlib`, `.so`, `.dll`, `.dylib`.

When `-t` is omitted, `-o` determines the type from its extension. With neither,
the output is an executable named after the root module, with an `.exe` suffix on
Windows targets. Default shared-library names use the target convention:
`module.dll`, `libmodule.dylib`, or `libmodule.so`.

Relocatable object output is unavailable for Windows targets. `-run` is valid
only for executables. It forwards the program's exit code and removes the
generated executable after the run.

### 28.4 Optimization and diagnostics

| Option | Meaning |
| --- | --- |
| `-Olevel` or `-O level` | Set `0`, `1`, `2`, `3`, `s`, `z`, `fast`, or `g`; default `2`. |
| `-v` | Print verbose build progress. |
| `-d` | Enable compiler debug detail. |

Numeric optimization levels above 3 are accepted, warned about, and treated as `-O3`.

### 28.5 Target and toolchain control

| Option | Meaning |
| --- | --- |
| `-target triple` | Set the compilation and C ABI target triple. |
| `-sysroot path` | Set the target sysroot for final linking. |
| `-cpu name` | Select the target CPU; `native` resolves to the host CPU. |
| `-features list` | Set comma-separated target features such as `+avx2,-sse4.1`. |
| `-target-abi name` | Set the target-specific ABI name. |
| `-relocation-model model` | Select `default`, `static`, `pic`, or `dynamic-no-pic`; the implicit default is PIC. |
| `-code-model model` | Select `default`, `tiny`, `small`, `kernel`, `medium`, or `large`. |
| `-Xlink "args"` | Append whitespace-split arguments to the native link. |
| `-static` | Request static linking; invalid for shared output. |
| `-nolibc` | Build without libc; Linux x86-64 executables use QK's freestanding startup. |
| `-lname` / `-l name` | Link a system library; repeatable. |
| `-Lpath` / `-L path` | Add a library search directory; repeatable. |

Arbitrary compilation arguments are intentionally unsupported; use the structured
options above. Cross-linking requires compatible CRT objects, libraries, and a
sysroot outside QK. `-target` does not install a cross toolchain.

On Linux x86-64, an executable built with `-nolibc` is linked with `-nostdlib`
and does not depend on C runtime startup. It terminates through the Linux `exit`
syscall. `panic` writes through the Linux `write` syscall and exits with status
101. There is currently no argument/environment entry API, and QK `main` returns
exit status 0 normally.

Freestanding executable startup is currently rejected for other targets rather
than producing a binary that depends on a platform CRT. Library and
object `-nolibc` workflows retain their existing linker behavior. Code reached by
a freestanding executable must not call libc-backed facilities such as
`std.print`, `std.println`, or builtin `Display` methods.

### 28.6 Informational options

`-h` and `--help` print usage. `--version` prints the development version string.

## 29. Diagnostics and troubleshooting

Diagnostics include a file, line, column, source excerpt, and marker. Several
independent errors can be reported in one run. Warnings do not prevent output
unless an error also occurs.

## 30. Examples

### 30.1 Calling C from an executable

```qk
module main

let puts(text: cstr): i32 @foreign

let main() {
    puts(c"hello from QK")
}
```

```sh
qkc .
./main
```

### 30.2 Multiple modules

`math_helpers.qk`:

```qk
module helpers

pub let square(value: i64): i64 = value * value
```

`main.qk`:

```qk
module main
import helpers h

let main() {
    let result = h.square(12)
}
```

Both files may live anywhere below the selected source directory. The import, not
the path, establishes the dependency.

### 30.3 Aggregates, methods, and iteration

```qk
module geometry

pub let Point = type struct {
    x: f64
    y: f64
}

pub let Point.translated(self, dx = 0.0, dy = 0.0: f64): Point {
    return Point { x = self.x + dx, y = self.y + dy }
}

pub let translate_all(mut points: [Point], dx, dy: f64) {
    for i in 0..len(points) {
        points[i] = points[i].translated(dx, dy)
    }
}
```

### 30.4 Tagged union pattern

```qk
module values

pub let ValueKind = type enum { integer, real }

pub let Value = type struct {
    kind: ValueKind
    union {
        integer: i64
        real: f64
    }
}

pub let integer_value(value: i64): Value = Value {
    kind = .integer
    integer = value
}
```

QK does not enforce the relationship between the tag and union field; the program
maintains that invariant.

### 30.5 Opaque foreign handle and cleanup

```qk
module files

pub let FILE = type opaque

let fopen(path: cstr, mode: cstr): *FILE @foreign
let fclose(file: *FILE): i32 @foreign

pub let process(path: cstr): bool {
    let file = fopen(path, c"rb")
    if file == nil {
        return false
    }
    defer fclose(file)

    // Read and process the file here.
    return true
}
```

### 30.6 Function pointer

```qk
module callbacks

let Operation = type alias *(i32, i32): i32

let add(a, b: i32): i32 = a + b

let apply(operation: Operation, a, b: i32): i32 {
    return operation(a, b)
}

let example(): i32 = apply(add, 2, 3)
```

### 30.7 Exporting a shared library API

```qk
module arithmetic

let add(a, b: i32): i32 @export = a + b
let library_version(): u32 @export("arithmetic_version") = 1
```

```sh
qkc . -m arithmetic -t so -o libarithmetic.so
```

Both exported functions use the target C ABI by default.

### 30.8 Module-controlled native linking

```qk
module compression @link(system "z")

let zlibVersion(): cstr @foreign

pub let version(): cstr = zlibVersion()
```

The importing executable or library automatically receives the module's `-lz`
requirement when `compression` is reachable from its root module.

### 30.9 Object output and cross-compilation

```sh
qkc src -m core -t obj -O3 -o core.o

qkc src \
    -m main \
    -target aarch64-unknown-linux-gnu \
    -sysroot /opt/aarch64-sysroot \
    -O2 \
    -o application
```

## 31. Current limitations

QK is intentionally small and currently has no:

- Generics, templates, trait generics, or inheritance.
- Closures or captured local functions.
- Exceptions, coroutines, async functions, or stack unwinding.
- Macro or compile-time metaprogramming system.
- Classes, constructors, destructors, or operator overloading.
- Ownership/lifetime checker, garbage collector, or automatic memory management.
- Package manager, dependency manifest, or integrated build graph beyond modules.
- Runtime reflection or dynamic type information.
- Packed structs, bit fields, or explicit layout attributes.
- Raw strings, string interpolation, Unicode escapes, numeric suffixes, or
  non-decimal floating-point literals.
- Implicit signed/unsigned integer mixing.
- Increment/decrement operators.
- General runtime bounds, overflow, null, or cast checks.
- Stable language, native ABI, IR, or compiler-plugin interface.

Target pointer width is supported for recognised 32-bit and 64-bit architectures.
C aggregate parameters and returns are supported for SysV AMD64, Windows x64,
and AArch64, but not yet for 32-bit targets. Scalar C ABI and ordinary QK calls
work on supported 32-bit targets.

`as` is reserved but casts use `value.(Type)`. Block comments do not nest. QK
strings and C strings are deliberately distinct.
