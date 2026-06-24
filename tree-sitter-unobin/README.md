# tree-sitter-unobin

Tree-sitter grammar and editor query files for Unobin source.

## Scripts

Run these commands from this directory:

```sh
npm run generate
npm run test
npm run compile
```

- `npm run generate` regenerates parser sources from `grammar.js`.
- `npm run test` runs the corpus tests under `test/corpus`.
- `npm run compile` builds the shared parser library used by editor installs.

## Queries

Editor query files live under `queries/`:

- `queries/highlights.scm` for syntax highlighting.
- `queries/locals.scm` for binding and reference queries.
- `queries/tags.scm` for outline and tag extraction.
- `queries/folds.scm` for fold ranges.

## Generated files

The generated files under `src/` are committed with grammar changes so editor
packages can build the parser without running generation first. Regenerate them
with `npm run generate` after changing `grammar.js`, then run the test and
compile scripts above.
