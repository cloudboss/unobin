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
}
```

Pointer fields are optional. For non-pointer fields that may be omitted, declare
`Defaults()` with `defaults.Value` or `defaults.Optional`. Output structs use the same
field naming rules. An output field can be marked sensitive:

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
        defaults.Optional(f.Tags),
    }
}
```

`defaults.Value` fills a value before the type's runtime method runs.
`defaults.Optional` says the field may be omitted and the zero value is acceptable.
The same defaults method model applies to library configuration structs.

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
