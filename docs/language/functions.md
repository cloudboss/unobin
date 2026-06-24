# Builtin functions

Builtin functions live under `@core`.

| Function | Meaning |
| --- | --- |
| `@core.join(list, sep)` | Join scalar list elements with a separator. |
| `@core.to-json(value)` | Render compact JSON. |
| `@core.b64-encode(string)` | Base64-encode a string. |
| `@core.b64-decode(string)` | Base64-decode a string. |
| `@core.range(n)` | Return integers from `0` through `n - 1`. |
| `@core.length(value)` | Return the length of a string, list, or map. |
| `@core.all(list(boolean))` | Return true when every element is true. |
| `@core.any(list(boolean))` | Return true when at least one element is true. |
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
