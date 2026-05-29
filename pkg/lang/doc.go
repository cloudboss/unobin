// Package lang holds the unobin source language: PEG grammar, lexer, AST.
//
// Scope: parsing of main.ub, category-prefixed library type bodies, and
// config.ub into a typed AST. Multi-error reporting with line/column from
// the parser.
//
// Companion packages:
//   - pkg/types - type system and type-expression evaluation
//   - pkg/codegen - AST to Go source
//   - pkg/resolve - import resolution and lock file
package lang
