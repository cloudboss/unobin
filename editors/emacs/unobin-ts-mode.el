;;; unobin-ts-mode.el --- Tree-sitter mode for Unobin -*- lexical-binding: t; -*-

;; Copyright (C) 2026 Joseph Wright

;; Author: Joseph Wright <rjosephwright@gmail.com>
;; Keywords: languages, tools
;; Version: 0.1.0
;; Package-Requires: ((emacs "29.1") (eglot "1.17"))
;; URL: https://github.com/cloudboss/unobin

;;; Commentary:

;; Tree-sitter major mode and eglot integration for Unobin source files.

;;; Code:

(require 'subr-x)
(require 'treesit nil t)

(defvar eglot-server-programs)
(defvar treesit-language-source-alist)
(defvar treesit-font-lock-feature-list)
(defvar treesit-font-lock-settings)
(defvar treesit-simple-indent-rules)

(defgroup unobin nil
  "Editor support for Unobin."
  :group 'languages)

(defcustom unobin-treesit-auto-install 'ask
  "How `unobin-ts-mode' handles a missing Tree-sitter grammar.
When nil, show a message with the install command.  When `ask', ask once per
Emacs session.  When t, install automatically."
  :type '(choice (const :tag "Do not install" nil)
                 (const :tag "Ask once" ask)
                 (const :tag "Install automatically" t))
  :group 'unobin)

(defcustom unobin-eglot-auto-start nil
  "When non-nil, start eglot automatically in `unobin-ts-mode' buffers."
  :type 'boolean
  :group 'unobin)

(defconst unobin-ts-mode--language 'unobin)

(defconst unobin-ts-mode--release-source
  '("https://github.com/cloudboss/unobin" nil "tree-sitter-unobin"
    "tree_sitter_unobin"))

(defconst unobin-ts-mode--fallback-highlights
  "
(comment) @font-lock-comment-face

[(string) (interpolated_string)] @font-lock-string-face

(field_key) @font-lock-property-name-face

(selector (identifier) @font-lock-function-name-face)

(call function: (identifier) @font-lock-function-name-face)
(call function: (path) @font-lock-function-name-face)

(identifier) @font-lock-variable-name-face
")

(defvar unobin-ts-mode--install-asked nil)

(defvar unobin-ts-mode--indent-rules
  `((,unobin-ts-mode--language
     ((node-is "}") parent-bol 0)
     ((parent-is "object") parent-bol 2)
     ((parent-is "block") parent-bol 2)
     (no-node parent-bol 0)))
  "Tree-sitter indentation rules for `unobin-ts-mode'.")

(defun unobin-ts-mode--repo-root ()
  "Return the repository root when this file is in a checkout."
  (when-let* ((file (or load-file-name buffer-file-name))
              (dir (file-name-directory file))
              (root (expand-file-name "../.." dir)))
    (when (file-directory-p (expand-file-name "tree-sitter-unobin" root))
      root)))

(defun unobin-ts-mode--local-grammar-dir ()
  "Return the local grammar directory when this file is in a checkout."
  (when-let* ((root (unobin-ts-mode--repo-root))
              (grammar (expand-file-name "tree-sitter-unobin" root)))
    (when (file-directory-p grammar)
      grammar)))

(defun unobin-ts-mode--query-file (name)
  "Return the local Tree-sitter query file named NAME when available."
  (when-let* ((root (unobin-ts-mode--repo-root))
              (path (expand-file-name
                     (concat "tree-sitter-unobin/queries/" name) root)))
    (when (file-readable-p path)
      path)))

(defun unobin-ts-mode--highlight-query ()
  "Return the highlight query for `unobin-ts-mode'."
  (if-let* ((path (unobin-ts-mode--query-file "highlights.scm")))
      (with-temp-buffer
        (insert-file-contents path)
        (buffer-string))
    unobin-ts-mode--fallback-highlights))

(defun unobin-ts-mode--font-lock-settings ()
  "Return Tree-sitter font-lock settings for `unobin-ts-mode'."
  (when (fboundp 'treesit-font-lock-rules)
    (treesit-font-lock-rules
     :language unobin-ts-mode--language
     :feature 'highlight
     (unobin-ts-mode--highlight-query))))

(defun unobin-ts-mode--grammar-recipe ()
  "Return the Tree-sitter grammar recipe for Unobin."
  (if-let* ((grammar (unobin-ts-mode--local-grammar-dir)))
      (cons unobin-ts-mode--language
            (list grammar nil nil "tree_sitter_unobin"))
    (cons unobin-ts-mode--language unobin-ts-mode--release-source)))

(defun unobin-ts-mode--with-grammar-recipe (body)
  "Call BODY with the Unobin Tree-sitter grammar recipe available."
  (let ((treesit-language-source-alist
         (cons (unobin-ts-mode--grammar-recipe) treesit-language-source-alist)))
    (funcall body)))

;;;###autoload
(defun unobin-install-treesit-grammar ()
  "Install the Unobin Tree-sitter grammar."
  (interactive)
  (unless (fboundp 'treesit-install-language-grammar)
    (user-error "This Emacs build does not support Tree-sitter grammars"))
  (unobin-ts-mode--with-grammar-recipe
   (lambda ()
     (treesit-install-language-grammar unobin-ts-mode--language))))

(defun unobin-ts-mode--grammar-ready-p ()
  "Return non-nil when the Unobin Tree-sitter grammar is available."
  (and (fboundp 'treesit-ready-p)
       (treesit-ready-p unobin-ts-mode--language t)))

(defun unobin-ts-mode--ensure-grammar ()
  "Ensure the Unobin Tree-sitter grammar is available when configured."
  (cond
   ((not (fboundp 'treesit-ready-p))
    (message "This Emacs build does not support Tree-sitter grammars")
    nil)
   ((unobin-ts-mode--grammar-ready-p) t)
   ((eq unobin-treesit-auto-install t)
    (unobin-install-treesit-grammar)
    (unobin-ts-mode--grammar-ready-p))
   ((eq unobin-treesit-auto-install 'ask)
    (unless unobin-ts-mode--install-asked
      (setq unobin-ts-mode--install-asked t)
      (when (y-or-n-p "Install the Unobin Tree-sitter grammar? ")
        (unobin-install-treesit-grammar)))
    (unobin-ts-mode--grammar-ready-p))
   (t
    (message "Unobin grammar missing; run M-x unobin-install-treesit-grammar")
    nil)))

(defun unobin-ts-mode--register-eglot ()
  "Register the Unobin language server with eglot."
  (with-eval-after-load 'eglot
    (add-to-list 'eglot-server-programs
                 '(unobin-ts-mode . ("unobin" "lsp")))))

(defun unobin-ts-mode--maybe-start-eglot ()
  "Start eglot when `unobin-eglot-auto-start' asks for it."
  (when (and unobin-eglot-auto-start (require 'eglot nil t))
    (eglot-ensure)))

(defun unobin-ts-mode--setup-treesit ()
  "Set up Tree-sitter parser, highlighting, and indentation."
  (when (unobin-ts-mode--ensure-grammar)
    (treesit-parser-create unobin-ts-mode--language)
    (setq-local treesit-font-lock-settings
                (unobin-ts-mode--font-lock-settings))
    (setq-local treesit-font-lock-feature-list '((highlight)))
    (setq-local treesit-simple-indent-rules unobin-ts-mode--indent-rules)
    (treesit-major-mode-setup)))

;;;###autoload
(define-derived-mode unobin-ts-mode prog-mode "Unobin"
  "Major mode for Unobin source files."
  (setq-local comment-start "# ")
  (setq-local comment-end "")
  (unobin-ts-mode--register-eglot)
  (unobin-ts-mode--setup-treesit)
  (unobin-ts-mode--maybe-start-eglot))

;;;###autoload
(add-to-list 'auto-mode-alist '("\\.ub\\'" . unobin-ts-mode))

(provide 'unobin-ts-mode)

;;; unobin-ts-mode.el ends here
