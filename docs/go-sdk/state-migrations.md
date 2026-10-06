# State and migrations

Snapshots use format version 2. Each `state.Entry` has one `Category`
(`resource`, `data-source`, or `action`) and a `Composite` boolean. The
boolean marks a composite call even when it has no child entries.

| Earlier Go classification / JSON `entry-kind` | `Category` | `Composite` |
| --- | --- | --- |
| `EntryLeaf` / `leaf` | `resource` | `false` |
| `EntryData` / `data-source` | `data-source` | `false` |
| `EntryAction` / `action` | `action` | `false` |
| `EntryLibraryCall` / `library-call` | The call's category | `true` |

`Entry.Type` and `EntryType` are removed. JSON always includes `category`
and `composite`, including `false` for primitive entries. Older snapshot
versions are rejected; recreate state before using a rebuilt factory.
Resource schema migrations do not convert older snapshot formats.

Only primitive resources have an external deletion or refresh lifecycle.
Data observations and completed actions remain available for inspection;
removing their declarations prunes their entries. Composite moves include
their descendants. `state.Binding.Export` still identifies the selected
library export and retains the JSON name `kind`.

Runtime selectors and graph lookups use these names:

| Meaning | Name | Existing Go API |
| --- | --- | --- |
| Node namespace | `runtime.Category`, `Node.Category()` | `NodeKind`, `Node.Kind` |
| Selected library implementation | `Node.Export()`, `state.Binding.Export` | `Node.Type` |
| Selected implementation in a plan graph | `StepNode.Export()` | `StepNode.ExportKind` |
| Unexpanded declaration lookup | `stateref.DeclarationAddress()` | `stateref.Template()` |

The category alias preserves the existing Go type identity. The methods read
the public fields directly, so each value has one writable owner. Plan graph
JSON keeps its existing `node-kind` and `kind` names.

Declaration addresses remove iteration keys from every segment while retaining
composite call prefixes. For example,
`resource.group['east']/resource.server['a']` becomes
`resource.group/resource.server`. Concrete plan and state addresses retain
their keys. Removing only the final key produces a base address, which can
still include keys in its parent segments.

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
