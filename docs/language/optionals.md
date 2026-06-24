# Optional values and narrowing

Unobin checks null strictly. A value of type `optional(T)` cannot be used where `T` is required until the source accounts for null.

## Declaring optional values

```
inputs: {
  greeting: { type: optional(string) }
  tls:      { type: optional(object({ cert: optional(string) })) }
}
```

A declaration default provides a value when the input is omitted. For an optional declaration, null follows the same default path:

```
size: { type: optional(integer), default: 3 }
```

## Null tests

A null test narrows the value where the test proves it is set:

```
banner: if input.greeting == null then 'hello' else input.greeting
```

The `else` branch sees `input.greeting` as `string`.

## Guarded navigation

Use `?.` to read through a maybe-null object:

```
input.tls?.cert
```

The result is still optional. Use `??` to provide a fallback:

```
input.tls?.cert ?? 'self-signed'
```

## Comprehension filters

A `when` filter can narrow element fields:

```
[ for u in input.upstreams : u.port when u.port != null ]
```

## Constraint guards

A constraint can use `when` to guard the `require` expression:

```
{
  kind:    predicate
  when:    input.upstreams != null
  require: @core.all([ for u in input.upstreams : u.port == null || u.port > 0 ])
  message: 'upstream ports must be positive'
}
```
