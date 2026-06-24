# Builtin types

Input declarations and composite inputs declare types.

## Atomic types

- `string`
- `number`
- `integer`
- `boolean`
- `null`
- `opaque`

`opaque` means the checker knows a value exists but does not check its fields.

## Containers

```
list(string)
map(integer)
tuple(string, integer)
object({ name: string, port: integer })
```

An object type declaration can define declaration objects for its fields:

```
object({
  host: string
  port: { type: optional(integer), default: 8080, minimum: 1024 }
})
```

An open object allows only declared fields to be referenced while permitting extra fields to exist:

```
open(object({ kind: string }))
```

Additional undeclared fields are opaque and cannot be referenced.

## Optional values

`optional(T)` accepts a `T`, null, or a missing value in an object field.

```
optional(string)
optional(list(object({ host: string, port: optional(integer) })))
```

Defaults can be written on declarations:

```
size: { type: optional(integer), default: 3 }
```

## Library configuration types

A Go library can expose a configuration schema. A factory input can use it with `library-config('<library-path>')`:

```
cloud: {
  type: library-config('github.com/example/cloud')
  default: {}
}
```

## Input declaration fields

The full declaration form is an object with `type:` and optional metadata:

```
name: {
  type:        string
  description: 'Stack name'
  default:     'dev'
  min-length:  2
  max-length:  32
  pattern:     '^[a-z][a-z0-9-]*$'
  enum:        ['dev', 'prod']
}
```

Numbers accept `minimum` and `maximum`. Strings accept `min-length`, `max-length`, `pattern`, `format`, and `enum` where the declared type supports them. Top-level inputs can use `@sensitive: true`.
