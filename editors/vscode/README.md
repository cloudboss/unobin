# Unobin for VS Code

This extension provides Unobin language support for Visual Studio Code.

## Features

- Starts `unobin lsp` for diagnostics, formatting, symbols, definitions,
  completions, and hover.
- Provides TextMate grammar highlighting for `.ub` files.
- Watches `.ub`, `.go`, `go.mod`, `project.ub`, and `project-lock.ub` files so
  the language server can refresh project data.

## Configuration

Set `unobin.path` when the `unobin` executable is not on `PATH`:

```json
{
  "unobin.path": "/path/to/unobin"
}
```

## Development

Install dependencies in this directory and compile before testing or packaging:

```sh
npm install
npm run compile
```

VS Code packaging runs `npm run compile` through the `vscode:prepublish` script.
