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

Resource migration runs before identity migration, so `Identity.Migrate` receives the resource
values in the current schema. The identity definition digest includes its canonical library
binding, scope, version, address selectors, and stable-ID declaration. For the same binding,
changing identity metadata without increasing its version is rejected.

Configuration has its own schema version and migration callback on `ConfigurationType`.
Recorded configuration is migrated and strictly decoded before it reaches a provider. Current
source defaults do not fill missing fields in recorded state; migrations must supply the complete
current value.

## Stored formats

Plan and snapshot bodies use format version `2`. `runtime.PlanFormatVersion` and
`state.CurrentFormatVersion` both equal `2`; the encrypted envelope remains version `1`.
Decoders reject unknown fields, duplicate keys, invalid payload combinations, and incorrect
content digests.

Version-1 bodies are obsolete alpha artifacts and fail with `obsolete alpha format; create a new
plan or state`. Resource migrations operate on V2 resource records; they do not convert old plan
or snapshot formats. There is no format converter or automatic state import.
