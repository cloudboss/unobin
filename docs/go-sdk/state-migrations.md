# State and migrations

A resource type declares its current persisted schema version:

```go
func (r *Bucket) SchemaVersion() int { return 2 }
```

When a prior state entry has an older schema version, the runtime asks the resource registration to migrate it before planning or applying.

Implement `runtime.Migrator` on the resource type:

```go
func (r *Bucket) Migrate(
    oldVersion int,
    prior runtime.MigrationState,
) (runtime.MigrationState, error) {
    switch oldVersion {
    case 1:
        prior.Inputs["name"] = prior.Inputs["bucket"]
        delete(prior.Inputs, "bucket")
        return prior, nil
    default:
        return runtime.MigrationState{}, fmt.Errorf("unsupported version %d", oldVersion)
    }
}
```

`runtime.MigrationState` contains both maps from the persisted entry:

- `Inputs`, the evaluated inputs from the last apply.
- `Outputs`, the resource outputs from the last apply.

Migrate the whole entry together. The returned entry is stamped with the current `SchemaVersion`.
