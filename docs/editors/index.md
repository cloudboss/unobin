# Editor setup

Unobin includes editor support for `.ub` files.

The CLI starts the language server:

```
unobin lsp
```

Editor packages use that same server for diagnostics, formatting, document symbols, definitions, completions, and hover.

The LSP does not fetch dependencies while editing. Run dependency commands outside the editor:

```
unobin deps get github.com/example/lib@v1.2.3
unobin deps sync
```

Use the editor package for your environment:

- [Emacs](emacs.md)
- [VS Code](vscode.md)
- [Tree-sitter](tree-sitter.md)
- [LSP](lsp.md)
