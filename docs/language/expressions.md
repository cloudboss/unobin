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
