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

func (f *File) SchemaVersion() int { return 1 }
func (f *File) ReplaceFields() []string { return []string{"path"} }

func (f *File) Create(ctx context.Context, cfg runtime.NoConfig) (*FileOutput, error) {
    return writeFile(f.Path, f.Content)
}

func (f *File) Read(
    ctx context.Context,
    cfg runtime.NoConfig,
    prior *FileOutput,
) (*FileOutput, error) {
    return readFile(prior)
}

func (f *File) Update(
    ctx context.Context,
    cfg runtime.NoConfig,
    prior runtime.Prior[File, *FileOutput],
) (*FileOutput, error) {
    return writeFile(f.Path, f.Content)
}

func (f *File) Delete(ctx context.Context, cfg runtime.NoConfig, prior *FileOutput) error {
    return removeFile(prior)
}
```

Register it:

```go
Resources: map[string]runtime.ResourceRegistration{
    "file": runtime.MakeResource[File, *FileOutput, runtime.NoConfig](),
}
```

Use `MakeResourceWith` when each receiver needs a constructed client, fake, or other external
state. The constructor runs each time the runtime needs a receiver, then the resource body is
decoded into that receiver.

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

Use `runtime.Changed(prior.Inputs.Field, current.Field)` to compare decoded values.

## Replace fields

`ReplaceFields` names input fields that require replacement when changed. Other changes call `Update`.

## Apply-time input validation

For checks that need the decoded library config or an external lookup, implement
`runtime.InputValidator[Config]` on the resource receiver:

```go
func (f *File) ValidateInputs(ctx context.Context, cfg runtime.NoConfig) error {
    if f.Path == "" {
        return errors.New("path is required")
    }
    return nil
}
```

The runtime calls `ValidateInputs` after decoding the desired inputs and before `Create`,
`Update`, or the create side of a replacement. For a replacement, validation runs before the
prior object is deleted. It is not called for no-op or destroy steps.

Prefer schemas, defaults, and constraints for checks that are known from source. Use
`ValidateInputs` for runtime checks that cannot be expressed in the compile-time input schema.

## Equivalent inputs

Implement `runtime.InputEquivalencer[In]` when two different input values represent the same
value for a resource field. For example, an AWS Lambda function where both its name and ARN
are equivalent:

```go
func (r *Alias) EquivalentInput(field string, prior, current Alias) bool {
	if field != "function-name" {
		return false
	}
	return equivalentFunctionNameOrARN(prior.FunctionName, current.FunctionName)
}

func equivalentFunctionNameOrARN(prior, current string) bool {
	if prior == current {
		return true
	}
	if name, ok := lambdaFunctionNameFromIdentifier(prior); ok && name == current {
		return true
	}
	if name, ok := lambdaFunctionNameFromIdentifier(current); ok && name == prior {
		return true
	}
	return false
}
```

`field` is the Unobin field name from the resource body. If the method returns true, the
field does not count as an input change for update planning, replacement triggers, or the
apply-time plan premise check. Return true only when the resource implementation treats
the two values the same.

## Resource plan modifiers

Implement `runtime.ResourcePlanModifier[In, Out, Config]` when a resource can tell the planner
that an output will be recomputed at apply:

```go
func (f *File) ModifyResourcePlan(
    req runtime.ResourcePlanRequest[File, *FileOutput, runtime.NoConfig],
    resp *runtime.ResourcePlanResponse,
) error {
    if req.HasPriorState && runtime.Changed(req.PriorInputs.Content, req.CurrentInputs.Content) {
        resp.MarkOutputUnknown("size")
    }
    return nil
}
```

The request contains the decoded config, prior inputs, current inputs, prior outputs, and whether
prior state exists. The runtime calls the method during planning for a resource with prior state,
after current inputs decode.

`MarkOutputUnknown` names output fields that apply will recompute. A later node that reads one
of those fields waits for apply instead of using the prior value in the plan. Marking an output
unknown also makes the resource a possible update unless replacement already applies.
