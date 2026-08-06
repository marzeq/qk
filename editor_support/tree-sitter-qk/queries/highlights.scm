; Comments and documentation markers
(line_comment) @comment
(block_comment) @comment
((line_comment) @comment.todo
  (#match? @comment.todo "(TODO|FIXME|XXX|NOTE)"))
((block_comment) @comment.todo
  (#match? @comment.todo "(TODO|FIXME|XXX|NOTE)"))

; Literals
(string_literal) @string
(c_string_literal) @string.special
(character_literal) @character
(escape_sequence) @string.escape
(integer_literal) @number
(float_literal) @number.float
(boolean_literal) @boolean
(nil_literal) @constant.builtin
(no_initializer) @constant.builtin

; Declarations
(module_declaration name: (module_path) @module)
(import_spec path: (module_path) @module)
(import_spec alias: (identifier) @module)
(type_definition name: (type_identifier) @type.definition)
(generic_parameter name: (type_identifier) @type.parameter)
(function_name function: (identifier) @function)
(function_name method: (identifier) @function.method)
(function_name owner: (identifier) @type)
(binding_declaration name: (identifier) @variable)
(multi_binding_declaration name: (identifier) @variable)
(parameter_name name: (identifier) @variable.parameter)
(trait_parameter name: (identifier) @variable.parameter)
(match_expression binding: (identifier) @variable)
(pattern_binding name: (identifier) @variable)
(for_each_clause element: (identifier) @variable)
(for_each_clause index: (identifier) @variable)

; Types and members
(type_identifier) @type
(struct_field name: (identifier) @property)
(union_field name: (identifier) @property)
(payload_field name: (identifier) @property)
(enum_member name: (identifier) @constant)
(flags_member name: (identifier) @constant)
(union_variant name: (identifier) @constructor)
(field_initializer field: (identifier) @property)
(flag_initializer member: (identifier) @constant)
(field_expression field: (identifier) @property)
(enum_literal variant: (identifier) @constructor)
(variant_pattern variant: (identifier) @constructor)

; Calls and compiler facilities
(call_expression function: (identifier) @function.call)
(call_expression
  function: (field_expression field: (identifier) @function.method.call))
(generic_call_expression (identifier) @function.call)
(generic_call_expression
  (field_expression field: (identifier) @function.method.call))
(generic_field_expression field: (identifier) @property)
(builtin_name) @function.builtin
(attribute_name) @attribute
(assembly_keyword) @keyword

; Language words
[
  "let"
  "mut"
  "pub"
  "module"
  "import"
  "comptime"
] @keyword

[
  "type"
  "alias"
  "struct"
  "union"
  "enum"
  "trait"
  "dyn"
] @keyword.type

(type_keyword) @keyword.type
(opaque_type) @keyword.type

[
  "if"
  "else"
  "when"
  "match"
] @keyword.conditional

[
  "for"
  "in"
] @keyword.repeat

[
  "return"
  "defer"
] @keyword.return

(break_statement) @keyword.return
(continue_statement) @keyword.return

"as" @keyword.operator

; Operators and punctuation
[
  "="
  "+="
  "-="
  "*="
  "/="
  "%="
  "&="
  "|="
  "^="
  "<<="
  ">>="
  "=="
  "!="
  "<"
  ">"
  "<="
  ">="
  "+"
  "-"
  "*"
  "/"
  "%"
  "&"
  "|"
  "^"
  "~"
  "!"
  "&&"
  "||"
  ".."
  "=>"
] @operator

[
  "("
  ")"
  "["
  "]"
  "{"
  "}"
] @punctuation.bracket

(generic_parameters
  ["<" ">"] @punctuation.bracket
  (#set! priority 105))

(type_arguments
  ["<" ">"] @punctuation.bracket
  (#set! priority 105))

[
  ","
  ";"
  ":"
  "."
] @punctuation.delimiter
