# State and migrations

A resource definition declares its current persisted schema version and its
migration function:

```go
func bucketDefinition() runtime.ResourceDefinition[
    Bucket,
    *BucketOutput,
    runtime.NoConfig,
] {
    return runtime.ResourceDefinition[Bucket, *BucketOutput, runtime.NoConfig]{
        SchemaVersion: 2,
        Migrate: func(
            oldVersion int,
            prior runtime.MigrationState,
        ) (runtime.MigrationState, error) {
            switch oldVersion {
            case 1:
                prior.Inputs["name"] = prior.Inputs["bucket"]
                delete(prior.Inputs, "bucket")
                return prior, nil
            default:
                return runtime.MigrationState{}, fmt.Errorf(
                    "unsupported version %d",
                    oldVersion,
                )
            }
        },
    }
}
```

When a prior state entry has an older schema version, the runtime calls the
migration before planning or applying.

`runtime.MigrationState` contains both maps from the persisted entry:

- `Inputs`, the evaluated inputs from the last apply.
- `Outputs`, the resource outputs from the last apply.

Migrate the whole entry together. The returned entry is stamped with the current
`SchemaVersion`.
