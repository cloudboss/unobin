# Unobin Emacs support

`unobin-ts-mode` is the Tree-sitter major mode for `.ub` files. It also
registers `unobin lsp` with eglot when eglot is loaded.

```elisp
(use-package unobin-ts-mode
  :ensure t
  :mode ("\\.ub\\'" . unobin-ts-mode)
  :custom
  (unobin-treesit-auto-install 'ask))
```

Completions come from eglot. Run `M-x eglot` in a `.ub` buffer, or start it
automatically in Unobin buffers:

```elisp
(use-package unobin-ts-mode
  :ensure t
  :mode ("\\.ub\\'" . unobin-ts-mode)
  :custom
  (unobin-eglot-auto-start t)
  (unobin-treesit-auto-install 'ask))
```

The first `.ub` buffer asks to install the Tree-sitter grammar by default. Run
`M-x unobin-install-treesit-grammar` to install it explicitly.

Manual highlight check: open a `.ub` file and verify that comments, strings,
declarations, selectors, type constructors, root names, and meta keys receive
faces from Tree-sitter highlighting.

Use `M-x completion-at-point` to check built-in completion. Popup completion
menus require a CAPF frontend such as Corfu or Company.

Keep the `unobin` executable on `PATH` so eglot can start `unobin lsp`.
