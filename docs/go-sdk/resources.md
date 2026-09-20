# Resources

A resource implements provider operations with
`runtime.TypedResource[In, Out, Config]`. Lifecycle policy belongs in one
`runtime.ResourceDefinition` passed during registration.

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

func fileDefinition() runtime.ResourceDefinition[
    File,
    *FileOutput,
    runtime.NoConfig,
] {
    path := runtime.InputField(func(input *File) *string {
        return &input.Path
    })
    return runtime.ResourceDefinition[File, *FileOutput, runtime.NoConfig]{
        SchemaVersion: 1,
        Replace: runtime.Replacement[File, *FileOutput, runtime.NoConfig]{
            Fields: []runtime.AnyInputField[File]{path},
        },
    }
}

func (f *File) Create(
    ctx context.Context,
    config runtime.NoConfig,
) (*FileOutput, error) {
    return writeFile(f.Path, f.Content)
}

func (f *File) Read(
    ctx context.Context,
    config runtime.NoConfig,
    prior runtime.Prior[File, *FileOutput, runtime.NoConfig],
) (*FileOutput, error) {
    return readFile(prior.Inputs.Path, prior.Outputs)
}

func (f *File) Update(
    ctx context.Context,
    config runtime.NoConfig,
    prior runtime.Prior[File, *FileOutput, runtime.NoConfig],
) (*FileOutput, error) {
    return writeFile(f.Path, f.Content)
}

func (f *File) Delete(
    ctx context.Context,
    config runtime.NoConfig,
    prior runtime.Prior[File, *FileOutput, runtime.NoConfig],
) error {
    return removeFile(prior.Inputs.Path, prior.Observed)
}
```

Register the resource with its definition:

```go
Resources: map[string]runtime.ResourceRegistration{
    "file": runtime.MakeResource[File, *FileOutput, runtime.NoConfig](
        fileDefinition(),
    ),
}
```

Use `MakeResourceWith` when each receiver needs a constructed client, fake, or
other external state. Pass the definition first and the constructor second.

## Prior target information

`runtime.Prior[In, Out, Config]` contains:

- `Inputs`, the recorded inputs for the managed target.
- `Outputs`, the outputs saved after the previous apply.
- `Observed`, the latest read result. It is zero during `Read`, the saved
  plan-time observation during `Update`, and the immediate observation during
  `Delete`.
- `Configuration`, the configuration recorded for the managed target.

The separate `Config` method argument is the current configuration. Use it for
credentials, endpoints, timeouts, and other settings needed to perform the
operation. Use `Prior.Configuration`, `Prior.Inputs`, and `Prior.Outputs` to
identify the previously managed target.

Return `runtime.ErrNotFound` from `Read` only when the recorded target is
absent. The runtime then plans `Create`.

## Definition

Every resource definition sets a positive `SchemaVersion`. It can also declare:

- `Migrate` for older state entries.
- `Validate` for checks that require decoded inputs or live configuration.
- `Equality` for field-specific semantic equality.
- `Replace` for input, configuration, and drift replacement rules.
- `StableID` for an immutable provider object identifier.

`Validate` runs before create, update, or replacement mutations. For a
replacement it runs before the old target is deleted.

```go
Validate: func(
    ctx context.Context,
    input File,
    config runtime.NoConfig,
) error {
    if input.Path == "" {
        return errors.New("path is required")
    }
    return nil
},
```

Prefer schemas, defaults, and constraints for checks known from source.

## Semantic equality

Use `runtime.EqualBy` when different input values mean the same thing to the
provider:

```go
functionName := runtime.InputField(func(input *Alias) *string {
    return &input.FunctionName
})

Equality: []runtime.InputEqualityRule[Alias]{
    runtime.EqualBy(functionName, equivalentFunctionNameOrARN),
},
```

Equivalent desired values do not request update or replacement. A successful
no-op stores the accepted value.

## Replacement

`Replacement.Fields` lists input fields whose semantic changes always require
replacement. `Replacement.Rules` applies a predicate after a selected input
field changes.

```go
capacity := runtime.InputField(func(input *Volume) *int64 {
    return &input.Capacity
})

Replace: runtime.Replacement[Volume, *VolumeOutput, *Config]{
    Rules: []runtime.ReplacementRule[Volume]{
        runtime.ReplaceWhen(capacity, func(prior, desired int64) bool {
            return desired < prior
        }),
    },
},
```

`ConfigurationFields` selects configuration changes that require replacement
for this resource. Credential changes otherwise produce a no-op when inputs and
remote state are unchanged.

`Drift` selects output changes that require replacement. A resource with drift
replacement rules must declare `StableID`:

```go
generation := runtime.OutputField(func(output *BucketOutput) *int64 {
    return &output.Generation
})

Replace: runtime.Replacement[Bucket, *BucketOutput, *Config]{
    Drift: []runtime.DriftRule[*BucketOutput]{
        runtime.ReplaceOnDrift(generation, func(recorded, observed int64) bool {
            return recorded != observed
        }),
    },
},
StableID: func(_ Bucket, output *BucketOutput) (string, error) {
    return output.ProviderID, nil
},
```

A stable ID must be non-empty, immutable for one provider object, and different
for a later object that reuses the same name. The runtime checks it during
planning and immediately before deletion.

Selectors are typed field references. Registration rejects invalid, duplicate,
or overlapping selectors and nil callbacks.
