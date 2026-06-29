# Schemas, constraints, defaults

The compiler reads Go library source to derive input schemas, configuration schemas,
output fields, constraints, and defaults.

## Struct fields

Input and configuration structs become UB object fields. Field names use kebab case by
default, and `ub` tags can set the UB name:

```go
type File struct {
    Path          string
    CreateParents bool `ub:"create-parents"`
    Tags          map[string]string
    MaybeTags     *map[string]string `ub:"maybe-tags"`
    Profile       *string
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
    }
}
```

Set constraints include `ExactlyOneOf`, `AtLeastOneOf`, `AtMostOneOf`,
`RequiredTogether`, `RequiredWith`, and `ForbiddenWith`. Predicate constraints use
`Must` or `When(...).Require(...)`. Library configuration structs use the same
constraints method model.

## Check timing

Deep schema and constraint checks happen at compile time when the source and selected
libraries are known. The compiled factory trusts those checks and decodes runtime
values into the registered Go types.

For checks that need live configuration or external state, a resource can also implement
`runtime.InputValidator`; see [Resources](resources.md#apply-time-input-validation).
