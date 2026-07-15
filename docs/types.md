# qk Types

The compiler currently supports these types.

## Primitive types

```text
i8  i16  i32  i64
u8  u16  u32  u64
f32 f64
isz usz
bool char void
```

`isz` and `usz` are the machine-sized integer types:

- `isz` = signed size integer
- `usz` = unsigned size integer

## Composite types

### Pointers

```text
*T
```

### Slices

```text
[T]
[T, N]
```

`N` is the fixed size of the slice/array form. The unsized form `[T]` is used when the length is not part of the type.

### Structs

```text
struct { x: i32, y: i32 }
```

### Functions

```text
(T1, T2): R
```

## Matching rules

- primitives match only the same primitive
- pointers match when their pointee types match
- slices match when their element types and sizes match
- structs match when they have the same fields in the same order
- functions match when their parameter lists and return type match

## Automatic conversion

The compiler will convert values automatically in a few cases:

- signed integers widen to larger signed integers
- unsigned integers widen to larger unsigned integers
- integers can become floats
- `isz` can fit into signed integers
- integers can become `usz`
- `f32` can widen to `f64`
- pointer values can be converted through `*void`

## Explicit casts

`as` allows conversions between:

- numeric types
- `bool` and integers
- `char` and integers
- integers and pointers
- pointers and pointers

## Untyped literals

Integer and float literals start untyped until the compiler decides on a concrete type.

- An untyped numeric literal is resolved by a type annotation, assignment, function argument, explicitly typed
  slice or struct field, or an arithmetic operation with a concrete numeric operand.
- A declaration without a concrete type context is rejected. Use a type annotation such as
  `let x: u32 = 3` or an explicit cast such as `let x = 3.(i64)`.
