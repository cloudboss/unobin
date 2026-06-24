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
  "then"
  "when"
] @font-lock-keyword-face

[
  "library-config"
  "list"
  "map"
  "object"
  "open"
  "optional"
  "tuple"
] @font-lock-type-face

(atomic_type) @font-lock-type-face

[
  (boolean)
  (null)
  (number)
] @font-lock-constant-face

[
  "!"
  "!="
  "&&"
  "*"
  "+"
  "-"
  "/"
  "<"
  "<="
  "=="
  ">"
  ">="
  "??"
  "=>"
  "||"
  "..."
] @font-lock-operator-face

[
  "("
  ")"
  "["
  "]"
  "{"
  "}"
] @font-lock-bracket-face

[
  ":"
  ","
  "."
  "?."
] @font-lock-delimiter-face

(field_key) @font-lock-property-name-face

(path (identifier) @font-lock-variable-name-face)
(binding (identifier) @font-lock-variable-name-face)
(primary_expression (identifier) @font-lock-variable-name-face)

((field_key (identifier) @font-lock-keyword-face)
 (#match? @font-lock-keyword-face "^(actions|configurations|constraints|data-sources|deps|encryption|factory|imports|inputs|library|library-configs|locals|outputs|parallelism|pin|project|project-lock|replace|requires|resources|stack|state|state-moves|toolchain|unobin-version|version)$"))

((field_key (identifier) @font-lock-preprocessor-face)
 (#match? @font-lock-preprocessor-face "^@"))

((path (identifier) @font-lock-builtin-face)
 (#match? @font-lock-builtin-face "^(@core|@each|@self|action|data-source|input|local|resource)$"))

(selector (identifier) @font-lock-function-name-face)

(call
  function: (identifier) @font-lock-function-name-face)

(call
  function: (path (identifier) @font-lock-function-name-face))
