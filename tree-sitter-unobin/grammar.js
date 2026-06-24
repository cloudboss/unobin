const PREC = {
  coalesce: 1,
  or: 2,
  and: 3,
  equality: 4,
  compare: 5,
  add: 6,
  multiply: 7,
  unary: 8,
  call: 9,
};

module.exports = grammar({
  name: 'unobin',

  extras: $ => [
    /[\s\uFEFF\u2060\u200B]/,
    $.comment,
  ],

  word: $ => $.identifier,

  conflicts: $ => [
    [$.selector, $.path],
  ],

  rules: {
    source_file: $ => repeat(choice(
      $.field,
      $.selector_body,
    )),

    comment: _ => token(seq('#', /.*/)),

    field: $ => choice(
      $.type_value_field,
      $.value_field,
    ),

    type_value_field: $ => seq(
      field('key', alias($.type_key, $.field_key)),
      ':',
      field('value', $.type_expression),
      optional(','),
    ),

    value_field: $ => seq(
      field('key', $.field_key),
      ':',
      field('value', choice($.selector_body_value, $.expression)),
      optional(','),
    ),

    selector_body: $ => seq(
      field('selector', $.selector),
      field('body', $.object),
      optional(','),
    ),

    selector_body_value: $ => seq(
      field('selector', $.selector),
      field('body', $.object),
    ),

    selector: $ => seq(
      $.identifier,
      repeat(seq('.', $.identifier)),
    ),

    field_key: $ => choice(
      $.identifier,
      $.quoted_key,
      $.dotted_key,
    ),

    dotted_key: $ => seq(
      $.identifier,
      repeat1(seq('.', $.identifier)),
    ),

    quoted_key: $ => $.string,

    expression: $ => choice(
      $.conditional,
      $.binary_expression,
      $.unary_expression,
      $.primary_expression,
    ),

    conditional: $ => seq(
      'if',
      field('condition', $.expression),
      'then',
      field('consequence', $.expression),
      'else',
      field('alternative', $.expression),
    ),

    binary_expression: $ => choice(
      prec.left(PREC.coalesce, seq($.expression, '??', $.expression)),
      prec.left(PREC.or, seq($.expression, '||', $.expression)),
      prec.left(PREC.and, seq($.expression, '&&', $.expression)),
      prec.left(PREC.equality, seq($.expression, choice('==', '!='), $.expression)),
      prec.left(PREC.compare, seq($.expression, choice('<', '<=', '>', '>='), $.expression)),
      prec.left(PREC.add, seq($.expression, choice('+', '-'), $.expression)),
      prec.left(PREC.multiply, seq($.expression, choice('*', '/'), $.expression)),
    ),

    unary_expression: $ => prec(PREC.unary, seq(
      choice('!', '-'),
      $.expression,
    )),

    primary_expression: $ => choice(
      $.map_comprehension,
      $.list_comprehension,
      $.object,
      $.array,
      $.call,
      $.path,
      $.identifier,
      $.string,
      $.interpolated_string,
      $.number,
      $.boolean,
      $.null,
      seq('(', $.expression, ')'),
    ),

    object: $ => seq(
      '{',
      repeat(choice($.field, $.selector_body)),
      '}',
    ),

    array: $ => seq(
      '[',
      optional(commaSep1($.expression)),
      optional(','),
      ']',
    ),

    list_comprehension: $ => seq(
      '[',
      'for',
      field('binding', $.binding),
      'in',
      field('source', $.expression),
      ':',
      field('value', $.expression),
      optional($.filter),
      ']',
    ),

    map_comprehension: $ => seq(
      '{',
      'for',
      field('binding', $.binding),
      'in',
      field('source', $.expression),
      ':',
      field('key', $.expression),
      '=>',
      field('value', $.expression),
      optional('...'),
      optional($.filter),
      '}',
    ),

    binding: $ => seq(
      $.identifier,
      optional(seq(',', $.identifier)),
    ),

    filter: $ => seq('when', $.expression),

    call: $ => prec(PREC.call, seq(
      field('function', choice($.path, $.identifier)),
      '(',
      optional(commaSep1($.expression)),
      optional(','),
      ')',
    )),

    path: $ => seq(
      $.identifier,
      repeat1(choice(
        seq('.', $.identifier),
        seq('?.', $.identifier),
        seq('[', choice('*', $.expression), ']'),
      )),
    ),

    type_expression: $ => choice(
      $.atomic_type,
      $.list_type,
      $.map_type,
      $.tuple_type,
      $.optional_type,
      $.open_type,
      $.object_type,
      $.library_config_type,
    ),

    type_key: _ => 'type',

    atomic_type: _ => choice(
      'string',
      'number',
      'integer',
      'boolean',
      'null',
      'opaque',
      'object',
    ),

    list_type: $ => seq('list', '(', $.type_expression, optional(','), ')'),

    map_type: $ => seq('map', '(', $.type_expression, optional(','), ')'),

    tuple_type: $ => seq(
      'tuple',
      '(',
      commaSep1($.type_expression),
      optional(','),
      ')',
    ),

    optional_type: $ => seq('optional', '(', $.type_expression, optional(','), ')'),

    open_type: $ => seq('open', '(', $.type_expression, optional(','), ')'),

    object_type: $ => seq(
      'object',
      '(',
      '{',
      repeat($.type_field),
      '}',
      optional(','),
      ')',
    ),

    type_field: $ => seq(
      field('key', $.field_key),
      ':',
      field('type', choice($.type_expression, $.type_input_decl)),
      optional(','),
    ),

    type_input_decl: $ => seq(
      '{',
      repeat($.field),
      '}',
    ),

    library_config_type: $ => seq(
      'library-config',
      '(',
      $.string,
      optional(','),
      ')',
    ),

    string: $ => choice(
      $.single_quoted_string,
      $.double_quoted_string,
      $.triple_quoted_string,
    ),

    single_quoted_string: _ => token(seq(
      "'",
      repeat(choice(/[^'\\\n\r]/, /\\./)),
      "'",
    )),

    double_quoted_string: _ => token(seq(
      '"',
      repeat(choice(/[^"\\\n\r]/, /\\./)),
      '"',
    )),

    triple_quoted_string: $ => choice(
      $.block_triple_quoted_string,
      $.single_line_triple_quoted_string,
    ),

    block_triple_quoted_string: $ => seq(
      "'''",
      $.triple_string_sigil,
      repeat($.triple_quoted_string_text),
      "'''",
    ),

    single_line_triple_quoted_string: _ => token(seq(
      "'''",
      repeat(choice(/[^'\n\r]/, /'[^'\n\r]/, /''[^'\n\r]/)),
      "'''",
    )),

    triple_string_sigil: _ => token(choice('|-', '|', '>-', '>', '\\-', '\\')),

    triple_quoted_string_text: _ => token.immediate(choice(
      /[^']+/,
      /'''[^,\n}\])]/,
      /'[^']/,
      /''[^']/,
    )),

    interpolated_string: $ => choice(
      $.single_interpolated_string,
      $.triple_interpolated_string,
    ),

    single_interpolated_string: $ => seq(
      "$'",
      repeat(choice(
        $.interpolation,
        $.escape_sequence,
        $.interpolated_string_text,
      )),
      "'",
    ),

    triple_interpolated_string: $ => seq(
      "$'''",
      optional(choice('|-', '|', '>-', '>', '\\-', '\\')),
      repeat(choice(
        $.interpolation,
        $.triple_interpolated_string_text,
      )),
      "'''",
    ),

    interpolation: $ => seq(
      '{{',
      field('value', $.expression),
      optional(seq(':', field('format', $.format_verb))),
      '}}',
    ),

    format_verb: _ => token(/[^}\s]+/),

    escape_sequence: _ => token.immediate(seq('\\', /./)),

    interpolated_string_text: _ => token.immediate(choice(
      /[^'{}\\\n\r]+/,
      '{',
      '}',
    )),

    triple_interpolated_string_text: _ => token.immediate(choice(
      /[^{}']+/,
      /'[^'\n]/,
      /''[^'\n]/,
      '{',
      '}',
    )),

    number: _ => token(seq(
      optional('-'),
      choice(/0|[1-9][0-9]*/, /[0-9]+\.[0-9]+/),
    )),

    boolean: _ => choice('true', 'false'),

    null: _ => 'null',

    identifier: _ => /@?[A-Za-z][A-Za-z0-9_-]*/,
  },
});

function commaSep1(rule) {
  return seq(rule, repeat(seq(',', rule)));
}
