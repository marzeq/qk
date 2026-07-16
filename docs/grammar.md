# qk Grammar (Current Parser Behavior)

This document describes the syntax accepted by the current `tokeniser` and `parser` packages. It is intentionally parser-accurate rather than aspirational.

## Notes

- Newlines and semicolons are statement separators in many positions.
- Module-qualified names use `module:name` (colon), not dot notation.
- Function definitions are expression-bodied or block-bodied.
- Types declared with `type` are nominal and distinct by default. `type alias` creates a transparent alias instead.
- Enum variants are scoped (`Color.RED`); `.RED` is accepted when a `Color` is expected.
- Enum variants may all use implicit ordinal values or all specify integer values with `=`, but the two forms cannot be mixed in one enum.
- Untagged unions are nominal types. A union literal initializes exactly one field; field access is unchecked and does not track which field is active.
- `if` can be a statement and an expression.
- `given { ... } -> expr` is an expression form.

## EBNF

```ebnf
(* ======================== *)
(* Lexical / separators     *)
(* ======================== *)

newline        = "\\n" ;
sep            = newline | ";" ;
opt_newlines   = { newline } ;

identifier     = ( "_" | letter ), { "_" | letter | digit } ;
decimal_int    = digit, { digit } ;
binary_int     = "0", ( "b" | "B" ), binary_digit, { binary_digit } ;
octal_int      = "0", ( "o" | "O" ), octal_digit, { octal_digit } ;
hex_int        = "0", ( "x" | "X" ), hex_digit, { hex_digit } ;
number         = [ "-" ], ( decimal_int | binary_int | octal_int | hex_int ) ;
string_lit     = '"', { string_char }, '"' ;
char_lit       = "'", char_char, "'" ;

(* float literals are parsed, not tokenised directly *)
float_lit      = [ "-" ], decimal_int, ".", [ decimal_int ]
               | ".", decimal_int ;

bool_lit       = "true" | "false" ;
nil_lit        = "nil" ;

(* ======================== *)
(* Program / top level      *)
(* ======================== *)

program        = { sep }, [ top_item, { sep, top_item } ], { sep } ;

top_item       = module_decl
               | import_decl
               | let_top_item
               | pub_top_item ;

pub_top_item   = "pub", let_top_item ;

module_decl    = "module", identifier, { opt_newlines, module_attribute } ;

module_attribute = links_attribute ;
links_attribute  = "@", "links", "(", opt_newlines,
                   link_entry,
                   { ",", opt_newlines, link_entry },
                   [ "," ], opt_newlines,
                   ")" ;
link_entry       = ( "lib" | "path" | "search" ), string_lit ;

import_decl    = "import", (
                   identifier
                 | "(", opt_newlines,
                     identifier,
                     { ( "," | newline ), opt_newlines, identifier },
                     [ "," | newline ], opt_newlines,
                   ")"
                 ) ;

let_top_item   = fn_def | declaration | type_alias ;

(* ======================== *)
(* Statements / blocks      *)
(* ======================== *)

block          = "{", opt_newlines,
                 [ statement, { sep, statement }, [ sep ] ],
                 "}" ;

statement      = declaration
               | fn_def
               | type_alias
               | assignment
               | index_assignment
               | pointer_assignment
               | call_stmt
               | module_access_stmt
               | if_stmt
               | for_stmt
               | control_stmt
               | block ;

if_stmt        = "if", opt_newlines, expression, opt_newlines, block,
                 { opt_newlines, "else", opt_newlines,
                   ( "if", opt_newlines, expression, opt_newlines, block
                   | block )
                 } ;

for_stmt       = "for", opt_newlines,
                 ( identifier, "in", opt_newlines, expression,
                   [ "..", [ "=" ], opt_newlines, expression ]
                 | stmt_or_expr, ";", opt_newlines,
                   expression, ";", opt_newlines, stmt_or_expr
                 ),
                 opt_newlines, block ;

stmt_or_expr   = statement | expression ;

control_stmt   = "break"
               | "continue"
               | "return", [ expression ] ;

(* ======================== *)
(* Declarations / functions *)
(* ======================== *)

declaration    = "let", [ "mut" ], identifier,
                 [ ":", type_expr ],
                 "=", opt_newlines, expression ;

type_alias     = "let", identifier, "=", "type", [ "alias" ], type_expr ;

fn_def         = "let", identifier,
                 "(", [ fn_param_list | "..." ], ")",
                 [ ":", type_expr ],
                 { function_attribute },
                 ( block | "=", opt_newlines, expression | foreign_attribute ) ;

function_attribute = "@", ( "inline" | "noinline" | "noreturn" )
                   | foreign_attribute
                   | "@", "export", [ "(", string_lit, ")" ] ;
foreign_attribute  = "@", "foreign", [ "(", string_lit, ")" ] ;

fn_param_list  = fn_param, { ",", fn_param } ;
fn_param       = [ "mut" ], identifier, ":", type_expr ;

assignment        = identifier, "=", opt_newlines, expression ;
index_assignment  = identifier, "[", expression, "]", "=", opt_newlines, expression ;
pointer_assignment= "*", expression, "=", opt_newlines, expression ;

call_stmt       = module_access, "(", [ arg_list ], ")" ;
module_access_stmt = module_access ;

(* ======================== *)
(* Expressions              *)
(* ======================== *)

expression     = as_cast ;

as_cast        = logical_or, [ "as", opt_newlines, type_expr ] ;

logical_or     = logical_and,
                 { "or", opt_newlines, logical_and } ;

logical_and    = logical_not,
                 { "and", opt_newlines, logical_not } ;

logical_not    = ( "not", opt_newlines, logical_not )
               | bitwise_or ;

bitwise_or     = bitwise_xor, { "|", opt_newlines, bitwise_xor } ;
bitwise_xor    = bitwise_and, { "^", opt_newlines, bitwise_and } ;
bitwise_and    = comparison, { "&", opt_newlines, comparison } ;

comparison     = shift,
                 { ( "==" | "!=" | "<" | ">" | "<=" | ">=" ),
                   opt_newlines, shift } ;

shift          = add_sub,
                 { ( "<<" | ">>" ), opt_newlines, add_sub } ;

add_sub        = mul_div,
                 { ( "+" | "-" ), opt_newlines, mul_div } ;

mul_div        = unary,
                 { ( "*" | "/" | "%" ), opt_newlines, unary } ;

unary          = ( "-" | "~" ), opt_newlines, unary
               | postfix ;

postfix        = term,
                 {
                   "[" , opt_newlines, expression, opt_newlines, "]"
                   | "(" [ arg_list ] ")"
                   | ".", opt_newlines, identifier
                 } ;

term           = "(", opt_newlines, expression, opt_newlines, ")"
               | if_expr
               | given_expr
               | sizeof_expr
               | struct_literal
               | slice_literal
               | module_access
               | enum_shorthand
               | identifier
               | bool_lit
               | nil_lit
               | number
               | float_lit
               | string_lit
               | char_lit ;

if_expr        = "if", opt_newlines, expression, opt_newlines, block_expr,
                 { opt_newlines, "else", opt_newlines,
                   ( "if", opt_newlines, expression, opt_newlines, block_expr
                   | block_expr )
                 } ;

given_expr     = "given", opt_newlines, block, opt_newlines,
                 "->", opt_newlines, expression ;

block_expr     = "{", opt_newlines, expression, opt_newlines, "}" ;

sizeof_expr    = "sizeof", opt_newlines, type_expr ;

arg_list       = expression, { ",", opt_newlines, expression } ;

module_access  = identifier, [ ":", opt_newlines, identifier ] ;

struct_literal = [ module_access ],
                 "{", opt_newlines,
                 [ field_init,
                   { ( "," | newline ), opt_newlines, field_init },
                   [ "," | newline ]
                 ],
                 "}" ;

field_init     = identifier, "=", opt_newlines, expression ;

enum_shorthand = ".", identifier ;

slice_literal  = "[", expression,
                 { ( "," | newline ), opt_newlines, expression },
                 [ "," | newline ],
                 "]" ;

(* ======================== *)
(* Types                    *)
(* ======================== *)

type_expr      = pointer_type | slice_type | struct_type | union_type | enum_type | named_type ;

pointer_type   = "*", type_expr ;

slice_type     = "[", opt_newlines, type_expr,
                 [ ",", opt_newlines, number ],
                 opt_newlines, "]" ;

struct_type    = "struct", "{", opt_newlines,
                 [ struct_member,
                   { ( "," | newline ), opt_newlines, struct_member },
                   [ "," | newline ]
                 ],
                 "}" ;

struct_member  = struct_field | union_type ;

struct_field   = identifier, ":", type_expr ;

union_type     = "union", "{", opt_newlines,
                 struct_field, { ",", opt_newlines, struct_field },
                 [ "," ], opt_newlines, "}" ;

enum_type      = "enum", "{", opt_newlines,
                 enum_variant, { ",", opt_newlines, enum_variant },
                 [ "," ], opt_newlines, "}" ;

enum_variant   = identifier, [ "=", number ] ;

named_type     = identifier
               | identifier, ":", identifier ;
```

## Markdown Description

### 1. Tokenisation model

- Identifiers start with a letter or underscore and then continue with letters, digits, or underscore.
- Keywords include: `let`, `mut`, `struct`, `union`, `enum`, `type`, `alias`, `if`, `else`, `given`, `for`, `in`, `break`, `continue`, `return`, `import`, `module`, `pub`, `and`, `or`, `not`, `true`, `false`, `nil`, `as`, `sizeof`.
- Integer tokens are decimal with optional leading minus.
- Floating-point literals are assembled by the parser from integer tokens separated by a dot (for example `12.34`, `12.`, `.34`).
- Strings and chars support escape sequences: `\\`, `\"`, `\n`, `\r`, `\t`, `\b`, `\f`, `\v`, `\a`, `\0`.
- `//` and `/* ... */` comments are ignored by the tokeniser.
- A backslash followed by newline is treated as line continuation and skipped.

### 2. Program structure

- A file is a sequence of top-level items separated by newline or semicolon.
- Accepted top-level forms are: `module`, `import`, and `let ...` definitions/declarations/type aliases.
- `pub` is only accepted before `let ...` forms that result in function, declaration, or type-alias nodes.

### 3. Functions

- Function syntax is based on `let`:
  - `let name(args) = expr`
  - `let name(args): Ret = { ... }`
- Parameters are `name: Type` with optional `mut`.
- Variadic marker `...` is accepted in parameter lists.
- Foreign functions use the `@foreign` attribute and have no body:
  - `let puts(...): i32 @foreign("puts")`
- Variadic functions must be foreign.
- Defined functions can expose a linker symbol with `@export` (using the local function name) or `@export("symbol_name")`.

### 4. Statements and separators

- Inside blocks, statements are generally separated by newline or semicolon.
- `if` and `for` manage their own internal block boundaries and do not require an additional separator immediately after their parse in some contexts.
- `for` header accepts a semicolon-separated list of statements or expressions before the loop body block.

### 5. Expressions and precedence

From lowest to highest precedence:

1. `as` cast
2. `or`
3. `and`
4. `not`
5. comparisons: `== != < > <= >=`
6. addition/subtraction: `+ -`
7. multiplication/division/modulo: `* / %`
8. unary: `- * &`
9. postfix indexing and slice-length postfix `[]`
10. terms (literals, names, calls, struct/slice literals, parenthesized expressions, `if` expression, `given` expression, `sizeof`)

### 6. Types

- Named type: `T` or module-qualified `mod:T`.
- Pointer type: `*T`.
- Slice/array-like type form: `[T]` or `[T, N]` where `N` is a decimal integer.
- Struct type literal: `struct { field: Type, ... }`.

### 7. Parser-specific caveats

- Compound assignment tokens (`+=`, `-=`, `*=`, `/=`, `%=`) are tokenised but are not currently parsed as assignment statements.
- Slice literal parsing currently requires at least one element in practice.
- A module-qualified access uses `:` consistently for both value and type positions.
