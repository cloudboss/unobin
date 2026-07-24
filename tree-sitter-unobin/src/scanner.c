#include "tree_sitter/parser.h"

#include <stdbool.h>
#include <stddef.h>

enum TokenType {
  BLOCK_TRIPLE_QUOTED_STRING,
};

static void advance(TSLexer *lexer) {
  lexer->advance(lexer, false);
}

static void skip(TSLexer *lexer) {
  lexer->advance(lexer, true);
}

static bool scan_quote(TSLexer *lexer) {
  if (lexer->lookahead != '\'') {
    return false;
  }
  advance(lexer);
  return true;
}

static bool scan_opening(TSLexer *lexer) {
  if (!scan_quote(lexer) || !scan_quote(lexer) || !scan_quote(lexer)) {
    return false;
  }
  if (lexer->lookahead != '|' &&
      lexer->lookahead != '>' &&
      lexer->lookahead != '\\') {
    return false;
  }
  advance(lexer);
  if (lexer->lookahead == '-') {
    advance(lexer);
  }
  while (lexer->lookahead == ' ' || lexer->lookahead == '\t') {
    advance(lexer);
  }
  if (lexer->lookahead == '\r') {
    advance(lexer);
  }
  if (lexer->lookahead != '\n') {
    return false;
  }
  advance(lexer);
  return true;
}

static bool is_close_follower(int32_t character) {
  return character == 0 ||
         character == '\n' ||
         character == '\r' ||
         character == ' ' ||
         character == '\t' ||
         character == ',' ||
         character == ')' ||
         character == '}' ||
         character == ']' ||
         character == '#';
}

static bool scan_closing_line(TSLexer *lexer) {
  while (lexer->lookahead == ' ' || lexer->lookahead == '\t') {
    advance(lexer);
  }
  if (!scan_quote(lexer) || !scan_quote(lexer) || !scan_quote(lexer)) {
    return false;
  }
  if (!is_close_follower(lexer->lookahead)) {
    return false;
  }
  lexer->mark_end(lexer);
  return true;
}

void *tree_sitter_unobin_external_scanner_create(void) {
  return NULL;
}

void tree_sitter_unobin_external_scanner_destroy(void *payload) {
  (void)payload;
}

unsigned tree_sitter_unobin_external_scanner_serialize(
  void *payload,
  char *buffer
) {
  (void)payload;
  (void)buffer;
  return 0;
}

void tree_sitter_unobin_external_scanner_deserialize(
  void *payload,
  const char *buffer,
  unsigned length
) {
  (void)payload;
  (void)buffer;
  (void)length;
}

bool tree_sitter_unobin_external_scanner_scan(
  void *payload,
  TSLexer *lexer,
  const bool *valid_symbols
) {
  (void)payload;
  if (!valid_symbols[BLOCK_TRIPLE_QUOTED_STRING]) {
    return false;
  }
  while (lexer->lookahead == ' ' ||
         lexer->lookahead == '\t' ||
         lexer->lookahead == '\n' ||
         lexer->lookahead == '\r') {
    skip(lexer);
  }
  if (!scan_opening(lexer)) {
    return false;
  }

  for (;;) {
    if (scan_closing_line(lexer)) {
      lexer->result_symbol = BLOCK_TRIPLE_QUOTED_STRING;
      return true;
    }
    while (lexer->lookahead != 0 && lexer->lookahead != '\n') {
      advance(lexer);
    }
    if (lexer->lookahead == 0) {
      return false;
    }
    advance(lexer);
  }
}
