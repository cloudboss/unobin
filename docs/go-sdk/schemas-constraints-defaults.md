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

### String format predicates

Use these conditions with `Must` or `When(...).Require(...)`:

| Condition | Check |
| --- | --- |
| `Matches(field, pattern)` | A match for a Go regular expression; use `^` and `$` for a full-string match. |
| `IsRegex(field)` | Valid Go regular expression syntax. |
| `IsIP(field)` | An IPv4 or IPv6 address. |
| `IsCIDR(field)` | An IPv4 or IPv6 CIDR prefix. |
| `IsBase64(field)` | A string that decodes as standard padded base64. |
| `IsPositiveDuration(field)` | A valid Go duration greater than zero. |

The field must be a string field, a nullable string field, or a string iteration
parameter. Each format condition passes for null. Pair it with `Present` or
`NotEmpty` when the value must be set or non-empty. Nested selectors can refer to
fields below optional objects; a missing object makes the selected field null.
See [String format predicates](../language/functions.md#string-format-predicates)
for format details.

`Matches` requires a pattern made from string literals, `+` concatenation, and
parentheses. For example, `"^" + ("[a-z][a-z0-9-]*") + "$"` is accepted. Named
constants, variables, and function calls are not accepted in that argument.
An invalid regular expression is a schema extraction error. This differs from
calling `@core.matches` directly, which returns false for an invalid pattern and
a non-null string value.

```go
type Endpoint struct {
    Name    *string
    Address *string
    Timeout *string
}

func (e Endpoint) Constraints() []constraint.Constraint {
    return []constraint.Constraint{
        constraint.Must(
            constraint.Present(e.Name),
            constraint.Matches(e.Name, "^" + "[a-z][a-z0-9-]*" + "$"),
        ).Message("name must start with a lowercase letter"),
        constraint.Must(constraint.IsIP(e.Address)),
        constraint.Must(constraint.IsPositiveDuration(e.Timeout)),
    }
}
```

### Map iteration

`ForEach` checks lists and maps. A list callback takes its element type; a map
callback takes `constraint.MapEntry[T]`, with `Key string` and `Value T` for the
map's value type. Both fields refer to the same entry, so one predicate can
compare them. Plain maps, named maps, and their pointer forms are supported.
Null maps have no entries, so they pass per-entry checks. Require presence or a
minimum size separately if an empty map must fail.

```go
type TaggedFile struct {
    Tags *map[string]string
}

func (f TaggedFile) Constraints() []constraint.Constraint {
    return []constraint.Constraint{
        constraint.ForEach(f.Tags, func(entry constraint.MapEntry[string]) []constraint.Constraint {
            return []constraint.Constraint{
                constraint.Must(
                    constraint.Matches(entry.Key, "^[a-z][a-z0-9-]*$"),
                    constraint.NotEmpty(entry.Value),
                ),
                constraint.When(constraint.Equals(entry.Key, "environment")).
                    Require(constraint.OneOf(entry.Value, "development", "production")),
            }
        }),
    }
}
```

Each iteration body must be a function literal with one named parameter and a
single return of a constraint list. The parameter must be `MapEntry[T]` (or a
type alias) with `T` matching the map's value type. It must not reuse the receiver
name or an enclosing iteration's parameter name. Use `entry.Value.Field` for
fields of struct values and `ForEach(entry.Value, ...)` to iterate a list or map
value. Iterations can nest with other map or list iterations and refer to
enclosing fields and parameters. Map iteration permits only predicates
(`Must` and `When(...).Require(...)`), including inside nested list iterations.
Set constraints such as `ExactlyOneOf` are rejected within map iteration.
Attach `Message` to rules inside the body, not to the iteration itself.

### Library API compatibility

Libraries that use the string format conditions or map iteration declare
`Compatibility: runtime.LibraryCompatibility{RequiredAPI: "1.1"}` in their
`Library()` return value. This lets tools that implement only API `1.0` reject
the library before schema extraction. The current implementation supports API
`1.1` and continues to accept libraries that require `1.0`. Existing libraries
that use only API `1.0` contracts can keep that requirement. See
[Maintaining compatibility](libraries.md#maintaining-compatibility).

## Check timing

The compiler derives schemas and constraints from the selected library source
and checks values that are known at compile time. During planning, each native
resource, data source, or action constraint runs as soon as all fields it reads
are resolved. A rule that reads a pending dependency waits for apply; unrelated
rules still run during planning.

At apply, the compiled factory resolves leaf inputs, applies their declared
defaults, and checks the native constraints before decoding inputs and calling
the leaf's lifecycle methods. These checks also run when applying a saved plan
and for resource steps with a `NoOp` decision. A violation stops that leaf before
its lifecycle call. Destroy does not recheck historical input constraints.

For checks that need live configuration or external state, set `Validate` on the
resource definition; see [Resources](resources.md#definition).
