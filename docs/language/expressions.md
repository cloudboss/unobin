# Expressions

Expressions produce values for inputs, locals, node bodies, constraints, and outputs.

## Literals

```
'web'
3
1.5
true
false
null
['a', 'b']
{ name: 'web', port: 443 }
```

## References

Factory expressions can read these roots:

- `input.<name>` for factory or composite inputs.
- `local.<name>` for file locals.
- `resource.<name>` for resource outputs.
- `data-source.<name>` for data source outputs.
- `action.<name>` for action outputs.
- `asset.<name>` for a file or directory captured by the current body.
- `@each.key` and `@each.value` inside `@for-each` and iterating constraints.
- `@core.<name>(...)` for builtin functions.
- `<alias>.<name>(...)` for imported functions, where `<alias>` is the import alias.

```
resource.web.id
input.instances[0]
input.tags['Name']
```

A node with `@for-each` gets one `@each` value per item:

```
resources: {
  file: std.fs-file {
    @for-each: input.files
    path:      $'/tmp/{{ @each.key }}.txt'
    content:   @each.value
  }
}
```

## Asset references

An asset reference starts with the `asset` root and a name declared in the
current factory or composite body:

```
asset.lambda.path
asset.lambda.content
asset.lambda.content-sha256
asset.lambda.mode
```

For a directory asset, one bracket may select a descendant. Put the complete
canonical relative path in that bracket:

```
asset.lambda['main.go'].content
asset.lambda['internal/helpers.go'].path
asset.lambda['internal'].content-sha256
```

The selection must be a string literal using `/`, with no empty, `.` or `..`
segments. Computed selection, repeated brackets, and selection from a file asset
are invalid.

Asset paths support `==` and `!=` with other path or string expressions. The
comparison uses the logical reference, not a host cache path. Asset content
compares as bytes and can be passed to byte-compatible functions such as
`@core.b64-encode`. Equality between disjoint types, such as asset content and a
string, is a type mismatch.

## Operators

Common operators include arithmetic, comparisons, boolean operators, `?.`, and `??`:

```
input.count + 1
input.environment == 'prod'
input.enabled && input.count > 0
input.tls?.cert ?? 'self-signed'
```

Infix operators must have even spacing. Write `a - b` or `a + b`, not `a -b` or `a+ b`. Due to kebab-casing in identifiers, `a-b` would be evaluated as an identifier, not subtraction.

## Function calls

Functions are qualified by their import alias. Builtins use the `@core` namespace:

```
@core.length(input.names)
@core.join(input.names, ',')
```

Imported Go library functions are qualified by their import alias:

```
imports: { text: 'github.com/example/text' }

locals: {
  slug: text.slug(input.name)
}
```
