(comment) @font-lock-comment-face

[
  (string)
  (interpolated_string)
] @font-lock-string-face

[
  "else"
  "for"
  "if"
  "in"
  "library-config"
  "list"
  "map"
  "object"
  "open"
  "optional"
  "then"
  "tuple"
  "when"
] @font-lock-keyword-face

((identifier) @font-lock-keyword-face
 (#any-of? @font-lock-keyword-face
  "action"
  "actions"
  "data-source"
  "data-sources"
  "factory"
  "imports"
  "inputs"
  "library"
  "local"
  "locals"
  "outputs"
  "project"
  "project-lock"
  "replace"
  "requires"
  "resource"
  "resources"
  "stack"
  "type"))

(atomic_type) @font-lock-type-face

(field_key) @font-lock-property-name-face

(selector
  (identifier) @font-lock-function-name-face)

(call
  function: (identifier) @font-lock-function-name-face)

(call
  function: (path) @font-lock-function-name-face)

(identifier) @font-lock-variable-name-face
