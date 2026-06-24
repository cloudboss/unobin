# Emacs

`unobin-ts-mode` is the Tree-sitter major mode for `.ub` files. It also registers `unobin lsp` with Eglot when Eglot is loaded.

```elisp
(use-package unobin-ts-mode
  :ensure t
  :mode ("\\.ub\\'" . unobin-ts-mode)
  :custom
  (unobin-treesit-auto-install 'ask))
```

To start Eglot automatically in Unobin buffers:

```elisp
(use-package unobin-ts-mode
  :ensure t
  :mode ("\\.ub\\'" . unobin-ts-mode)
  :custom
  (unobin-eglot-auto-start t)
  (unobin-treesit-auto-install 'ask))
```

The first `.ub` buffer asks to install the Tree-sitter grammar by default. Run `M-x unobin-install-treesit-grammar` to install it explicitly.

Completions come from Eglot. Run `M-x eglot` in a `.ub` buffer, then use `M-x completion-at-point` or a CAPF frontend such as Corfu or Company.

Keep the `unobin` executable on `PATH` so Eglot can start `unobin lsp`.
