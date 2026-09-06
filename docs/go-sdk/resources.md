# Resources

A resource manages CRUD operations on an external object. It is implemented as
an instance of the `runtime.TypedResource[In, Out, Config any]` interface.

The following is a `runtime.TypedResource[File, *FileOutput, runtime.NoConfig]`:

```go
type File struct {
    Path    string
    Content string
}

type FileOutput struct {
    Path   string
    Size   int64
    Exists bool
}

func (f *File) Create(ctx context.Context, cfg runtime.NoConfig) (*FileOutput, error) {
    return writeFile(f.Path, f.Content)
}

func (f *File) Read(
    ctx context.Context,
    cfg runtime.NoConfig,
    prior *FileOutput,
) (*FileOutput, error) {
    return readFile(f.Path)
}

func (f *File) Update(
    ctx context.Context,
    cfg runtime.NoConfig,
    prior runtime.Prior[File, *FileOutput],
) (*FileOutput, error) {
    return writeFile(f.Path, f.Content)
}

func (f *File) Delete(ctx context.Context, cfg runtime.NoConfig, prior *FileOutput) error {
    return removeFile(f.Path)
}
```

Declare the schema and logical address, then register the definition:

```go
func fileDefinition() runtime.ResourceDefinition[File, *FileOutput, runtime.NoConfig] {
    return runtime.ResourceDefinition[File, *FileOutput, runtime.NoConfig]{
        SchemaVersion: 1,
        Identity: runtime.ResourceIdentity[File, *FileOutput]{
            Version: 1,
            Scope: runtime.IdentityConfiguration,
            AddressInputs: []runtime.AnyInputField[File]{
                runtime.InputField(func(f *File) *string { return &f.Path }),
            },
        },
    }
}

Resources: map[string]runtime.ResourceRegistration{
    "file": runtime.MakeResource[File, *FileOutput, runtime.NoConfig](fileDefinition()),
}
```

Inputs must be a struct and outputs must be a pointer to a struct. A resource with no output
fields uses `*struct{}`. Successful Create, Read, and Update calls must return a non-nil output.
Registration validates the complete definition and panics if it is invalid, before constructing
any provider receiver. Both schema and identity versions must be positive.

Use `MakeResourceWith` when each receiver needs a constructed client, fake, or other external
state. The constructor runs each time the runtime needs a receiver, then the resource body is
decoded into that receiver. Pass the definition first and the constructor second.

## Read and not found

Return `runtime.ErrNotFound` from `Read` when the external object is absent. The runtime treats
that as a request to create it again.

`Read` runs during planning for resources that already have state. `Create`, `Update`, `Delete`,
and replacement work run only during apply.

## Update

`runtime.Prior[In, Out]` includes:

- `Inputs`, the prior evaluated inputs.
- `Outputs`, the prior resource outputs.
- `Observed`, the plan-time read result.

Update receives the saved observation without an implicit apply-time Read. If the definition
declares a stable ID, Update must return that same ID; a different ID fails before state is saved.

Use `runtime.Changed(prior.Inputs.Field, current.Field)` to compare decoded values.

Every Update output becomes pending during planning. Dependents wait for the provider's fresh
result, including when an output has the same name as an input.

## Replacement rules

Address inputs identify the remote object. Changing one requires replacement. Other immutable
inputs can use typed replacement rules:

```go
definition.Replacement.Inputs = []runtime.ReplacementRule[Bucket]{
    runtime.ReplaceWhenChanged(runtime.InputField(func(b *Bucket) *string { return &b.Region })),
    runtime.ReplaceWhen(
        runtime.InputField(func(b *Bucket) *int64 { return &b.Capacity }),
        func(prior, desired int64) bool { return desired < prior },
    ),
}
```

The selector must return a field of its supplied root. Registration checks nested selectors,
supported field types, duplicates, and conflicting rules. An address input cannot also have an
equality or replacement rule.

## Stable identity and drift

Declare `Identity.StableID` when the provider exposes an immutable object ID or incarnation token:

```go
definition.Identity.StableID = func(_ Bucket, out *BucketOutput) (string, error) {
    return out.ID, nil
}
```

The callback must return a non-empty value. Use the provider's ID for the particular object,
since a reusable name cannot distinguish a deleted object from a new object at the same address.
Without this callback, Unobin uses the declared logical address for identity and deletion.

Ordinary remote drift selects Update. A drift rule can request replacement when an observed
output differs from its recorded value:

```go
definition.Replacement.Drift = []runtime.DriftRule[*BucketOutput]{
    runtime.ReplaceOnDrift(
        runtime.OutputField(func(out *BucketOutput) *int64 { return &out.Capacity }),
        func(recorded, observed int64) bool { return recorded == observed },
    ),
}
```

Drift replacement requires a stable ID; registration rejects a drift rule without one. Collection
selectors compare the entire map or list. Nested selectors may traverse struct pointers, but
cannot select individual collection elements.

## Replacement execution and failures

Replacement deletes the prior object before creating the desired object. Immediately before
Delete, Unobin reads the prior target using its recorded inputs and configuration. A changed
stable ID stops the operation before Delete; `ErrNotFound` skips Delete and proceeds to Create.

Delete failure prevents Create. Create failure, invalid identity, or a state-write failure stops
the step without recovery or adoption. The failed resource's recorded state can still describe
the deleted object, and a partially created object can exist outside state. Inspect the provider
and state, resolve any partial objects, then create a new plan. Work already running follows the
apply scheduler's failure behavior; no additional steps are dispatched after failure is reported.

## Apply-time input validation

For checks that need the decoded library config or an external lookup, set `Validate`:

```go
definition.Validate = func(ctx context.Context, f File, cfg runtime.NoConfig) error {
    if f.Path == "" {
        return errors.New("path is required")
    }
    return nil
}
```

The runtime calls `Validate` after decoding the desired inputs and before `Create`,
`Update`, or the create side of a replacement. For a replacement, validation runs before the
prior object is deleted. It is not called for no-op or destroy steps.

Prefer schemas, defaults, and constraints for checks that are known from source. Use
`Validate` for runtime checks that cannot be expressed in the compile-time input schema.
Capture clients or test counters in the callback closure; private receiver fields are not decoded
resource inputs.

## Equivalent inputs

Use `EqualBy` when two different values have the same meaning for a non-address input:

```go
definition.InputSemantics.Rules = []runtime.InputRule[Bucket]{
    runtime.EqualBy(
        runtime.InputField(func(b *Bucket) *map[string]string { return &b.Labels }),
        maps.Equal,
    ),
}
```

Semantic equality suppresses an input change during planning and is checked before conditional
replacement. Apply still compares concrete reviewed inputs exactly; semantic equality cannot
authorize a different input at apply time.

A successful NoOp saves the accepted desired inputs with the observed outputs and identity, so
the next plan compares against the values that were accepted.
