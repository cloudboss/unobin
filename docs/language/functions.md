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
| `@core.is-match(value, pattern)` | Match a string against a Go regular expression. |
| `@core.is-format(value, format)` | Check a string against a named format. |
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

## String predicates

Both predicates accept a string or null. Null passes. Use a presence rule when
the field must be set.

`@core.is-match(value, pattern)` searches for a Go regular expression match.
Use `^` and `$` for a full-string match. An invalid pattern returns false for a
string value.

`@core.is-format(value, format)` accepts the literal format names below.
Malformed values return false, and unknown format names produce a schema error.

| Format | Check |
| --- | --- |
| `regex` | Valid Go regular expression syntax. |
| `ip` | IPv4 or IPv6 address. |
| `cidr` | IPv4 or IPv6 prefix. Host bits may be set. |
| `base64` | Standard padded base64. |
| `positive-duration` | Go duration greater than zero. |
| `date-time` | RFC3339 date and time. |
| `json` | Valid JSON text. |
| `int64` | Base-10 signed 64-bit integer text. Signs and leading zeros are allowed. |
