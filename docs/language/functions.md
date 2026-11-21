# Builtin functions

Builtin functions live under `@core`.

| Function | Meaning |
| --- | --- |
| `@core.join(list, sep)` | Join scalar list elements with a separator. |
| `@core.to-json(value)` | Render compact JSON. |
| `@core.b64-encode(string \| bytes)` | Base64-encode a string or bytes. |
| `@core.b64-decode(string)` | Base64-decode a string. |
| `@core.range(n)` | Return integers from `0` through `n - 1`. |
| `@core.length(value)` | Return the length of a string, list, or map. |
| `@core.all(list(boolean))` | Return true when every element is true. |
| `@core.any(list(boolean))` | Return true when at least one element is true. |
| `@core.matches(optional(string), string) -> boolean` | Test a string against a Go regular expression. |
| `@core.is-regex(optional(string)) -> boolean` | Test whether a string is a valid Go regular expression. |
| `@core.is-ip(optional(string)) -> boolean` | Test whether a string is an IPv4 or IPv6 address. |
| `@core.is-cidr(optional(string)) -> boolean` | Test whether a string is an IPv4 or IPv6 CIDR prefix. |
| `@core.is-base64(optional(string)) -> boolean` | Test whether a string decodes as standard padded base64. |
| `@core.is-positive-duration(optional(string)) -> boolean` | Test whether a string is a positive Go duration. |
| `@core.merge(objects...)` | Merge objects left to right. |
| `@core.to-integer(value)` | Convert a number or numeric string to an integer. |
| `@core.to-number(value)` | Convert an integer or numeric string to a number. |
| `@core.to-string(value)` | Render a scalar as text. |
| `@core.to-boolean(value)` | Convert true, false, or their string forms to a boolean. |

Calls are qualified:

```
@core.length(input.names)
@core.join(input.names, ',')
```

Go libraries can also export functions. Call them with the import alias:

```
net.cidr-host(input.cidr, 10)
```

Function calls run inline during expression evaluation. They do not create state nodes.

## String format predicates

The six format predicates above accept a string or `null` as their first argument
and return a boolean. They return `true` for `null`, so optional fields can omit
a value without failing a format check. Require presence separately when needed.
A non-string, non-null argument is a type error; it is not converted to text.

`@core.matches(value, pattern)` uses Go regular expression syntax and searches for
a match anywhere in the string. Use `^` and `$` to require a full-string match.
The pattern must be a non-null string. An invalid pattern returns `false` when
the value is a string; a null value still returns `true`.
`@core.is-regex(value)` tests the syntax of the value itself.

The other predicates return `false` for strings that fail their format check:

- `is-ip` accepts IPv4 and IPv6 addresses, without a port or CIDR suffix.
- `is-cidr` accepts an IPv4 or IPv6 address followed by a valid prefix length.
  Host bits need not be zero: `192.0.2.1/24` passes.
- `is-base64` uses Go's standard base64 decoder, including its padding rules.
  It accepts the empty string and ignores carriage returns and newlines.
- `is-positive-duration` accepts Go duration strings such as `250ms` and `1h30m`.
  Zero, negative durations, invalid strings, and durations outside Go's supported
  range fail.

```
@core.matches('app-42', '^app-[0-9]+$') # true
@core.matches('app-42', '[')          # false
@core.is-regex('[')                  # false
@core.is-ip('2001:db8::1')            # true
@core.is-cidr('192.0.2.1/24')         # true
@core.is-base64('aGk=')              # true
@core.is-positive-duration('30s')       # true
@core.is-positive-duration('0s')        # false
@core.matches(null, '[')             # true
```
