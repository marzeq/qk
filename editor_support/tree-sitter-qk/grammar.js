const PREC = {
  LOGICAL_OR: 1,
  LOGICAL_AND: 2,
  BITWISE_OR: 3,
  BITWISE_XOR: 4,
  BITWISE_AND: 5,
  COMPARISON: 6,
  SHIFT: 7,
  ADD: 8,
  MULTIPLY: 9,
  UNARY: 10,
  POSTFIX: 11,
};

module.exports = grammar({
  name: 'qk',

  word: $ => $.identifier,

  extras: $ => [
    /[\s\uFEFF\u2060\u200B]/,
    /\\\r?\n/,
    $.line_comment,
    $.block_comment,
  ],

  supertypes: $ => [
    $.expression,
    $.statement,
    $.type,
  ],

  conflicts: $ => [
    [$.binding_declaration, $.type_identifier],
    [$.function_name, $.binding_declaration, $.type_identifier],
    [$.expression, $.type_identifier],
    [$.expression, $._generic_reference, $.type_identifier],
    [$.expression, $._generic_reference],
    [$.import_spec],
    [$.for_each_clause, $.expression],
    [$.for_each_clause, $.expression, $.type_identifier],
    [$.for_statement, $.expression],
    [$.sequence_literal, $.slice_type],
    [$.sequence_literal, $.array_type],
  ],

  rules: {
    source_file: $ => repeat($._top_level_item),

    _top_level_item: $ => choice(
      $.module_declaration,
      $.import_declaration,
      $.function_definition,
      $.type_definition,
      $.binding_declaration,
      $.multi_binding_declaration,
      $.compile_time_when,
      $.compiler_error_directive,
      $.attribute,
      ';',
    ),

    module_declaration: $ => prec.right(seq(
      'module',
      field('name', $.module_path),
      repeat(field('attribute', $.attribute)),
    )),

    import_declaration: $ => seq(
      'import',
      choice(
        $.import_spec,
        seq('(', repeat(seq($.import_spec, optional(','))), ')'),
      ),
    ),

    import_spec: $ => seq(
      field('path', $.module_path),
      optional(field('alias', $.identifier)),
    ),

    module_path: $ => prec.left(seq(
      $.identifier,
      repeat(seq('.', $.identifier)),
    )),

    function_definition: $ => prec.right(seq(
      optional('pub'),
      'let',
      field('name', $.function_name),
      optional(field('type_parameters', $.generic_parameters)),
      field('parameters', $.parameter_list),
      optional(seq(':', field('return_type', $.return_type))),
      repeat(field('attribute', $.attribute)),
      optional(field('body', choice(
        $.block,
        seq('=', $.expression),
      ))),
    )),

    function_name: $ => choice(
      field('function', $.identifier),
      seq(
        field('owner', choice(
          $.identifier,
          seq($.identifier, $.generic_parameters),
          seq('(', $.type, ')'),
        )),
        '.',
        field('method', $.identifier),
      ),
    ),

    generic_parameters: $ => seq(
      '<',
      commaSep1($.generic_parameter),
      optional(','),
      '>',
    ),

    generic_parameter: $ => seq(
      field('name', $.type_identifier),
      optional(seq(':', field('constraint', $.type))),
    ),

    parameter_list: $ => seq(
      '(',
      optional(seq(
        commaSep1(choice(
          $.receiver_parameter,
          $.parameter,
          $.variadic_parameter,
        )),
        optional(','),
      )),
      ')',
    ),

    receiver_parameter: $ => choice(
      alias('self', $.identifier),
      seq('*', optional('mut'), alias('self', $.identifier)),
    ),

    parameter: $ => seq(
      commaSep1($.parameter_name),
      ':',
      optional('...'),
      field('type', $.type),
      optional(seq('=', field('default', $.expression))),
    ),

    parameter_name: $ => seq(
      optional('mut'),
      field('name', $.identifier),
      optional(seq('=', field('default', $.expression))),
    ),

    variadic_parameter: _ => '...',

    return_type: $ => choice(
      $.type,
      seq('(', commaSep1($.type), ')'),
    ),

    type_definition: $ => seq(
      optional('pub'),
      'let',
      field('name', $.type_identifier),
      optional(field('type_parameters', $.generic_parameters)),
      '=',
      'type',
      optional('alias'),
      field('value', $.type),
    ),

    binding_declaration: $ => prec.right(seq(
      optional('pub'),
      'let',
      optional('mut'),
      field('name', $.identifier),
      optional(field('type_parameters', $.generic_parameters)),
      optional(seq(':', field('type', $.type))),
      choice(
        seq(
          '=',
          optional('comptime'),
          field('value', $.expression),
          repeat(field('attribute', $.attribute)),
        ),
        repeat1(field('attribute', $.attribute)),
      ),
    )),

    multi_binding_declaration: $ => seq(
      'let',
      optional('mut'),
      field('name', $.identifier),
      ',',
      commaSep1(field('name', $.identifier)),
      '=',
      field('value', $.expression),
    ),

    block: $ => seq('{', repeat(seq($.statement, optional(';'))), '}'),

    statement: $ => choice(
      $.function_definition,
      $.type_definition,
      $.binding_declaration,
      $.multi_binding_declaration,
      $.assignment_statement,
      $.return_statement,
      $.break_statement,
      $.continue_statement,
      $.defer_statement,
      $.for_statement,
      $.compile_time_when,
      $.compiler_error_directive,
      $.attribute,
      $.expression_statement,
    ),

    assignment_statement: $ => seq(
      commaSep1(field('left', $.expression)),
      field('operator', choice(
        '=', '+=', '-=', '*=', '/=', '%=', '&=', '|=', '^=', '<<=', '>>=',
      )),
      field('right', $.expression),
    ),

    return_statement: $ => prec.right(seq('return', optional(commaSep1($.expression)))),
    break_statement: _ => 'break',
    continue_statement: _ => 'continue',
    defer_statement: $ => seq('defer', $.expression),
    expression_statement: $ => $.expression,

    if_expression: $ => prec.right(seq(
      'if',
      field('condition', $.expression),
      field('consequence', $.block),
      repeat(seq('else', 'if', field('condition', $.expression), field('consequence', $.block))),
      optional(seq('else', field('alternative', $.block))),
    )),

    match_expression: $ => seq(
      'match',
      field('subject', $.expression),
      optional(seq('as', field('binding', $.identifier))),
      '{',
      commaSep1($.match_arm),
      optional(','),
      '}',
    ),

    match_arm: $ => seq(
      field('pattern', $.match_pattern),
      optional(seq('if', field('guard', $.expression))),
      '=>',
      field('body', $.expression),
    ),

    match_pattern: $ => prec.left(seq(
      $.match_pattern_term,
      repeat(seq('|', $.match_pattern_term)),
    )),

    match_pattern_term: $ => choice(
      $.match_pattern_atom,
      seq(field('start', $.literal), '..', field('end', $.literal)),
    ),

    match_pattern_atom: $ => choice(
      alias('_', $.wildcard),
      $.literal,
      $.variant_pattern,
    ),

    variant_pattern: $ => seq(
      '.',
      field('variant', $.identifier),
      optional(seq('(', optional(seq(commaSep1($.pattern_binding), optional(','))), ')')),
    ),

    pattern_binding: $ => seq(
      optional(seq(field('field', $.identifier), '=')),
      field('name', $.identifier),
    ),

    for_statement: $ => seq(
      'for',
      optional(choice($.for_each_clause, $.for_clause, $.expression)),
      field('body', $.block),
    ),

    for_each_clause: $ => seq(
      field('element', $.identifier),
      optional(seq('.', '&', optional('mut'))),
      optional(seq(',', field('index', $.identifier))),
      'in',
      field('iterable', $.expression),
      optional(seq('..', optional('='), field('end', $.expression))),
      repeat($.iteration_attribute),
    ),

    for_clause: $ => prec.left(seq(
      optional(choice($.binding_declaration, $.assignment_statement, $.expression)),
      ';',
      optional($.expression),
      ';',
      optional(choice($.assignment_statement, $.expression)),
    )),

    iteration_attribute: $ => seq('@', alias('reversed', $.attribute_name)),

    compile_time_when: $ => prec.right(seq(
      'when',
      field('condition', $.expression),
      field('consequence', $.compile_time_block),
      repeat(seq('else', 'when', field('condition', $.expression), field('consequence', $.compile_time_block))),
      optional(seq('else', field('alternative', $.compile_time_block))),
    )),

    compile_time_block: $ => seq(
      '{',
      repeat(choice($.module_declaration, $.import_declaration, $.statement, ';')),
      '}',
    ),

    compiler_error_directive: $ => seq(
      '@', alias('compiler_error', $.builtin_name), '(', $.string_literal, ')',
    ),

    attribute: $ => prec.right(seq(
      '@',
      field('name', $.attribute_name),
      optional(seq('(', optional(seq(commaSep1($.attribute_argument), optional(','))), ')')),
    )),

    attribute_argument: $ => choice(
      $.string_literal,
      seq(field('name', $.identifier), field('value', $.string_literal)),
    ),

    expression: $ => choice(
      $.identifier,
      $.literal,
      $.no_initializer,
      $.parenthesized_expression,
      $.block,
      $.if_expression,
      $.match_expression,
      $.struct_literal,
      $.sequence_literal,
      $.builtin_expression,
      $.inline_assembly_expression,
      $.unary_expression,
      $.binary_expression,
      $.call_expression,
      $.field_expression,
      $.index_expression,
      $.slice_expression,
      $.reference_expression,
      $.dereference_expression,
      $.cast_expression,
      $.generic_call_expression,
      $.generic_field_expression,
      $.enum_literal,
    ),

    parenthesized_expression: $ => seq('(', $.expression, ')'),

    unary_expression: $ => prec.right(PREC.UNARY, seq(
      field('operator', choice('!', '-', '~')),
      field('operand', $.expression),
    )),

    binary_expression: $ => choice(
      binary($, PREC.LOGICAL_OR, '||'),
      binary($, PREC.LOGICAL_AND, '&&'),
      binary($, PREC.BITWISE_OR, '|'),
      binary($, PREC.BITWISE_XOR, '^'),
      binary($, PREC.BITWISE_AND, '&'),
      binary($, PREC.COMPARISON, choice('==', '!=', '<', '>', '<=', '>=')),
      binary($, PREC.SHIFT, choice(seq('<', '<'), seq('>', '>'))),
      binary($, PREC.ADD, choice('+', '-')),
      binary($, PREC.MULTIPLY, choice('*', '/', '%')),
    ),

    call_expression: $ => prec.left(PREC.POSTFIX, seq(
      field('function', $.expression),
      field('arguments', $.argument_list),
    )),

    generic_call_expression: $ => prec.left(PREC.POSTFIX, seq(
      field('function', $._generic_reference),
      field('arguments', $.argument_list),
    )),

    argument_list: $ => seq(
      '(',
      optional(seq(commaSep1($.expression), optional('...'), optional(','))),
      ')',
    ),

    field_expression: $ => prec.left(PREC.POSTFIX, seq(
      field('value', $.expression), '.', field('field', $.identifier),
    )),

    generic_field_expression: $ => prec.left(PREC.POSTFIX, seq(
      field('value', $._generic_reference), '.', field('field', $.identifier),
    )),

    index_expression: $ => prec.left(PREC.POSTFIX, seq(
      field('value', $.expression), '[', field('index', $.expression), ']',
    )),

    slice_expression: $ => prec.left(PREC.POSTFIX, seq(
      field('value', $.expression),
      '[',
      optional(field('start', $.expression)),
      ':',
      optional(field('end', $.expression)),
      ']',
    )),

    reference_expression: $ => prec.left(PREC.POSTFIX, seq(
      field('value', $.expression), '.', '&', optional('mut'),
    )),

    dereference_expression: $ => prec.left(PREC.POSTFIX, seq(
      field('value', $.expression), '.', '*',
    )),

    cast_expression: $ => prec.left(PREC.POSTFIX, seq(
      field('value', $.expression), '.', '(', field('type', $.type), ')',
    )),

    _generic_reference: $ => seq(
      field('value', choice($.identifier, $.field_expression)),
      field('type_arguments', $.type_arguments),
    ),

    enum_literal: $ => seq('.', field('variant', $.identifier)),

    struct_literal: $ => prec.dynamic(1, seq(
      choice(
        seq('.', '{'),
        seq(field('type', $.named_type), '.', '{'),
      ),
      optional(choice(
        seq(commaSep1($.field_initializer), optional(',')),
        seq(commaSep1($.flag_initializer), optional(',')),
      )),
      '}',
    )),

    field_initializer: $ => choice(
      seq(field('field', $.identifier), '=', field('value', $.expression)),
      $.no_initializer,
    ),

    flag_initializer: $ => seq('.', field('member', $.identifier)),

    sequence_literal: $ => seq(
      '[',
      optional(choice(
        seq(commaSep1($.expression), optional(',')),
        seq(field('value', $.expression), ';', field('count', $.expression)),
      )),
      ']',
    ),

    builtin_expression: $ => choice(
      seq('@', field('name', alias(choice('len', 'repr'), $.builtin_name)), '(', $.expression, ')'),
      seq('@', field('name', alias(choice('sizeof', 'alignof'), $.builtin_name)), '(', choice($.type, $.expression), ')'),
      seq('@', field('name', alias('offsetof', $.builtin_name)), '(', $.type, ',', $.identifier, ')'),
    ),

    inline_assembly_expression: $ => seq(
      '@', alias('asm', $.builtin_name),
      '(',
      field('template', $.string_literal),
      repeat(seq(',', $.assembly_operand)),
      optional(','),
      ')',
    ),

    assembly_operand: $ => choice(
      seq(alias('out', $.assembly_keyword), field('type', $.type), field('constraint', $.string_literal)),
      seq(alias('in', $.assembly_keyword), field('value', $.expression), field('constraint', $.string_literal)),
      seq(alias('clobber', $.assembly_keyword), field('name', $.string_literal)),
      alias('volatile', $.assembly_keyword),
    ),

    type: $ => choice(
      $.named_type,
      $.pointer_type,
      $.dynamic_trait_type,
      $.slice_type,
      $.array_type,
      $.function_type,
      $.struct_type,
      $.enum_type,
      $.flags_type,
      $.union_type,
      $.trait_type,
      $.opaque_type,
      $.representation_type,
    ),

    named_type: $ => seq($.type_path, optional($.type_arguments)),
    type_path: $ => prec.left(seq($.type_identifier, repeat(seq('.', $.type_identifier)))),
    type_arguments: $ => seq('<', commaSep1($.type), optional(','), '>'),

    pointer_type: $ => seq('*', optional('mut'), $.type),
    dynamic_trait_type: $ => seq('*', optional('mut'), 'dyn', $.named_type),
    slice_type: $ => seq('[', ']', optional('mut'), $.type),
    array_type: $ => seq('[', field('length', $.expression), ']', field('element', $.type)),
    function_type: $ => seq(
      '*',
      '(',
      optional(seq(commaSep1(seq(optional('...'), $.type)), optional(','))),
      ')',
      ':',
      field('return_type', $.type),
    ),

    struct_type: $ => seq(
      'struct',
      repeat($.attribute),
      '{',
      optional(seq(commaSep1($.struct_field), optional(','))),
      '}',
    ),

    struct_field: $ => choice(
      seq(field('name', $.identifier), ':', field('type', $.type)),
      $.union_type,
    ),

    enum_type: $ => seq(
      'enum', '{', optional(seq(commaSep1($.enum_member), optional(','))), '}',
    ),
    enum_member: $ => seq(field('name', $.identifier), optional(seq('=', field('value', $.integer_literal)))),

    flags_type: $ => seq(
      alias('flags', $.type_keyword),
      '(', field('underlying', $.named_type), ')',
      '{', optional(seq(commaSep1($.flags_member), optional(','))), '}',
    ),
    flags_member: $ => seq(field('name', $.identifier), optional(seq('=', field('value', $.expression)))),

    union_type: $ => seq(
      'union',
      optional(seq('(', field('tag', choice($.type, $.auto_attribute)), ')')),
      '{', optional(seq(commaSep1(choice($.union_field, $.union_variant)), optional(','))), '}',
    ),
    auto_attribute: $ => seq('@', alias('auto', $.attribute_name)),
    union_field: $ => seq(field('name', $.identifier), ':', field('type', $.type)),
    union_variant: $ => seq(
      field('name', $.identifier),
      optional(seq('(', optional(seq(commaSep1($.payload_field), optional(','))), ')')),
    ),
    payload_field: $ => choice(
      $.type,
      seq(field('name', $.identifier), ':', field('type', $.type)),
    ),

    trait_type: $ => seq(
      'trait', '{', repeat(seq($.trait_method, optional(choice(',', ';')))), '}',
    ),
    trait_method: $ => seq(
      'let',
      field('name', $.identifier),
      optional($.generic_parameters),
      '(',
      field('receiver', $.receiver_parameter),
      repeat(seq(',', $.trait_parameter)),
      optional(','),
      ')',
      optional(seq(':', field('return_type', $.return_type))),
      optional(field('body', choice($.block, seq('=', $.expression)))),
    ),
    trait_parameter: $ => seq(field('name', $.identifier), ':', field('type', $.type)),

    opaque_type: _ => 'opaque',
    representation_type: $ => seq('@', alias('reprof', $.builtin_name), '(', $.type, ')'),

    literal: $ => choice(
      $.integer_literal,
      $.float_literal,
      $.string_literal,
      $.c_string_literal,
      $.character_literal,
      $.boolean_literal,
      $.nil_literal,
    ),

    integer_literal: _ => token(choice(
      /0[bB][01]+/,
      /0[oO][0-7]+/,
      /0[xX][0-9a-fA-F]+/,
      /[0-9]+/,
    )),
    float_literal: _ => token(/[0-9]+\.[0-9]+/),
    string_literal: $ => seq('"', repeat(choice($.escape_sequence, $.string_content)), '"'),
    c_string_literal: $ => seq('c"', repeat(choice($.escape_sequence, $.string_content)), '"'),
    character_literal: $ => seq("'", choice($.escape_sequence, /[^'\\\n]/), "'"),
    string_content: _ => token.immediate(prec(1, /[^"\\\n]+/)),
    escape_sequence: _ => token.immediate(/\\[\\"'nrtbfva0]/),
    boolean_literal: _ => choice('true', 'false'),
    nil_literal: _ => 'nil',
    no_initializer: _ => '---',

    identifier: _ => /[A-Za-z_][A-Za-z0-9_]*/,
    type_identifier: $ => alias($.identifier, $.type_identifier),
    attribute_name: $ => alias($.identifier, $.attribute_name),

    line_comment: _ => token(seq('//', /[^\n]*/)),
    block_comment: _ => token(seq('/*', /[^*]*\*+([^/*][^*]*\*+)*/, '/')),
  },
});

function commaSep1(rule) {
  return seq(rule, repeat(seq(',', rule)));
}

function binary($, precedence, operator) {
  return prec.left(precedence, seq(
    field('left', $.expression),
    field('operator', operator),
    field('right', $.expression),
  ));
}
