# Schemas, constraints, defaults

The compiler reads Go library source to derive input schemas, configuration schemas,
output fields, constraints, and defaults.

## Struct fields

Input and configuration structs become Unobin object fields. Field names use kebab
case by default, and `ub` tags can set the Unobin name:

```go
type File struct {
    Path          string
    Mode          int
    CreateParents bool `ub:"create-parents"`
    Tags          map[string]string
    MaybeTags     *map[string]string `ub:"maybe-tags"`
    Profile       *string
    Labels        *[]string
}
```

Pointer fields are optional and accept explicit `null`. Non-pointer fields,
including maps and slices, are required unless `Defaults()` declares a real value
with `defaults.Value`. Output structs use the same field naming rules. An output
field can be marked sensitive:

```go
type SecretOutput struct {
    Label string
    Value string `ub:",sensitive"`
}
```

## Defaults

A type can declare defaults with a `Defaults` method:

```go
func (f File) Defaults() []defaults.Default {
    return []defaults.Default{
        defaults.Value(f.CreateParents, true),
        defaults.Value(f.Tags, map[string]string{}),
        defaults.NullableValue(f.Profile, "dev"),
    }
}
```

`defaults.Value` fills a non-pointer field when a body omits it. Use
`defaults.NullableValue` for a pointer field where omission should produce a
non-null default while explicit `null` remains null. Pointer fields with no default
express optional input without a default. For maps and slices, use composite
literals such as `map[string]string{}` or `[]string{"a", "b"}`. The same defaults
method model applies to library configuration structs.

## Constraints

A type can declare cross-field constraints:

```go
func (f File) Constraints() []constraint.Constraint {
    return []constraint.Constraint{
        constraint.Must(constraint.NotEmpty(f.Path)).Message("path is required"),
        constraint.Must(constraint.AtLeast(f.Mode, 0)).Message("mode must be non-negative"),
        constraint.ForEach(f.Labels, func(label string) []constraint.Constraint {
            return []constraint.Constraint{
                constraint.Must(constraint.NotEmpty(label)).
                    Message("labels must be non-empty"),
            }
        }),
    }
}
```

Set constraints include `ExactlyOneOf`, `AtLeastOneOf`, `AtMostOneOf`,
`RequiredTogether`, `RequiredWith`, and `ForbiddenWith`. Predicate constraints use
`Must` or `When(...).Require(...)`. `constraint.ForEach` accepts plain slices,
named slices, pointer slices, and pointers to named slices when the first argument
is a field selector. `goschema` validates the field is a list and that the body
parameter matches the element type.

Pointer slices derive `optional(list(T))` in Unobin syntax. Go-derived predicate
constraints use an explicit empty-list fallback, equivalent to
`@for-each: input.labels ?? []`. Hand-written `.ub` constraints must write that
fallback explicitly for optional lists; a bare `@for-each: input.labels` is
rejected when `labels` may be null. Library configuration structs use the same
constraints method model.

## Check timing

Deep schema and constraint checks happen at compile time when the source and selected
libraries are known. The compiled factory trusts those checks and decodes runtime
values into the registered Go types.

For checks that need live configuration or external state, a resource can also implement
`runtime.InputValidator`; see [Resources](resources.md#apply-time-input-validation).
