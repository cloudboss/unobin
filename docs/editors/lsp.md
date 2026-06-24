# LSP

Start the language server over stdio:

```
unobin lsp
```

For debugging, record JSON-RPC traffic and server events:

```
unobin lsp --trace trace.json --log server.log
```

Trace files can include source text. Treat them as local debugging artifacts.

The server provides:

- Diagnostics for parse, syntax, dependency, and type errors.
- Formatting for `.ub`, `project.ub`, and `project-lock.ub` files.
- Document symbols and definitions.
- Completion for source blocks, references, expected values, and input declarations.
- Hover where semantic information is available.

The server keeps a project cache for open workspaces. File changes to `.ub`, `.go`, `go.mod`, `project.ub`, and `project-lock.ub` refresh the cache. It does not fetch remote dependencies during editing.
