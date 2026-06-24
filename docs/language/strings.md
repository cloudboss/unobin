# Strings

Standard strings in Unobin are single quoted. Double quotes are not used.

```
name: 'web'
path: '/tmp/app'
```

## Escapes

Single quoted strings recognize these escapes:

| Escape | Meaning |
| --- | --- |
| `\\` | Backslash |
| `\'` | Single quote |
| `\n` | Newline |
| `\t` | Tab |
| `\r` | Carriage return |
| `\0` | NUL byte |

Other backslash escapes are parse errors. Use a raw triple quoted string for backslash-heavy text.

## Triple quoted strings

A triple quoted string on a single line does not need to escape single quotes:

```
message: '''Environment must be 'dev' or 'prod''''
```

Triple quoted strings over multiple lines must define a mode after the opening quotes.

Literal mode preserves line breaks:

```
body: '''|-
  line one
  line two
  '''
```

This evaluates to:

```
line one
line two
```

Folded mode turns line breaks into spaces:

```
summary: '''>-
  this is written
  over two source lines
  '''
```

This evaluates to `this is written over two source lines`.

Joined mode removes line breaks:

```
url: '''\-
  https://example.com/
  api/v1/users
  '''
```

This evaluates to `https://example.com/api/v1/users`.

The `-` suffix strips the final newline. Without it, the final newline is kept.

## Interpolation

Prefix a string with `$` to use interpolation slots, which are defined by `{{ ... }}`:

```
path: $'/srv/{{ input.name }}/config.json'
content: $'''>-
  name={{ input.name }}
  port={{ input.port }}
  '''
```

Each slot contains one expression and must stay on one line. A single brace is literal. To write a literal slot opener, escape the first brace:

```
sample: $'This is \{{ not-a-slot }}'
```
