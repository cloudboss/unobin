# Tree-sitter

The Tree-sitter grammar lives under `tree-sitter-unobin`.

Run grammar commands from that directory:

```
cd tree-sitter-unobin
npm run generate
npm run test
npm run compile
```

Query files live under `tree-sitter-unobin/queries`:

- `highlights.scm` for syntax highlighting.
- `locals.scm` for binding and reference queries.
- `tags.scm` for outline and tag extraction.
- `folds.scm` for fold ranges.

Generated parser files under `tree-sitter-unobin/src` are committed with grammar changes. After changing `grammar.js`, run generate, test, and compile.
