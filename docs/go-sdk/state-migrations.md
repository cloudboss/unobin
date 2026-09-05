# State and migrations

A resource definition declares its current persisted schema version and an optional migration:

```go
definition.SchemaVersion = 2
definition.Migrate = func(
    oldVersion int,
    prior runtime.ResourceMigrationState,
) (runtime.ResourceMigrationState, error) {
    if oldVersion != 1 {
        return runtime.ResourceMigrationState{}, fmt.Errorf("unsupported version %d", oldVersion)
    }
    inputs, ok := prior.Inputs.ObjectFields()
    if !ok {
        return runtime.ResourceMigrationState{}, errors.New("inputs must be an object")
    }
    inputs["name"] = inputs["bucket"]
    delete(inputs, "bucket")
    migrated, err := runtime.ObjectValue(inputs)
    if err != nil {
        return runtime.ResourceMigrationState{}, err
    }
    return runtime.ResourceMigrationState{Inputs: migrated, Outputs: prior.Outputs}, nil
}
```

`ResourceMigrationState` contains encoded `Inputs` and `Outputs` from the last successful apply.
Read older objects through their encoded fields instead of decoding them into the current Go
struct. Return both values at the current schema version. A missing migration or a migration
error prevents the runtime from using an older resource schema.

Resource schema versions and identity versions are independent. Increment the schema version
when persisted resource values need migration. Increment the identity version when the address
or stable-ID interpretation changes, and provide `Identity.Migrate` for older identity records.
