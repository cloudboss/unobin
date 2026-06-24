# VS Code

The VS Code extension provides Unobin language support for `.ub` files.

It starts `unobin lsp` for diagnostics, formatting, symbols, definitions, completions, and hover. It also provides TextMate highlighting.

Set `unobin.path` when the `unobin` executable is not on `PATH`:

```json
{
  "unobin.path": "/path/to/unobin"
}
```

The extension watches `.ub`, `.go`, `go.mod`, `project.ub`, and `project-lock.ub` files so the language server can refresh project data.

For extension development:

```
cd editors/vscode
npm install
npm run compile
```

Packaging runs `npm run compile` through the `vscode:prepublish` script.
