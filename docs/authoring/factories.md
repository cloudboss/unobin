# Factories

A factory source describes the stack family you want to compile into one executable.

A small factory has inputs, imports, resources, and outputs:

```
factory: {
  description: 'Writes a file.'

  inputs: {
    path:    { type: string }
    content: { type: string }
  }

  imports: { std: 'github.com/cloudboss/unobin-library-std' }

  resources: {
    file: std.fs-file {
      path:    input.path
      content: input.content
    }
  }

  outputs: {
    path: { value: resource.file.path }
  }
}
```

## Inputs and locals

Inputs are values passed to the factory from the stack file. Locals are file-scoped expressions computed from inputs, resources, data sources, actions, and other locals where dependencies permit it.

```
locals: {
  path: $'{{ input.base-path }}/app.conf'
}
```

## Assets

Use `assets:` to include regular files or directories in the compiled factory:

```
factory: {
  assets: {
    source:  './lambda'
    archive: './dist/lambda.zip'
  }
}
```

Each declaration must be a non-empty relative string literal. Its path is
resolved from the directory containing the `.ub` file that declares the factory
or composite. The resolved path must remain in that project and must not overlap
the compile output directory.

Asset directories include hidden and underscore-prefixed entries. Unobin does
not read `.gitignore` or other ignore files when capturing them. A declared
asset and every entry below it must be a regular file or directory; symlinks and
other filesystem entry types are rejected.

Reference a declaration through the singular `asset` root:

```
resources: {
  bundle: app.archive {
    source-dir:   asset.source.path
    archive-data: asset.archive.content
  }
}
```

Every asset value has these attributes:

| Attribute | Meaning |
| --- | --- |
| `path` | A logical path that becomes a local path when passed to Go code. |
| `content` | Exact file bytes, or a deterministic ZIP for a directory. |
| `content-sha256` | Lowercase hexadecimal SHA-256 of `content`. |
| `mode` | Normalized Unix mode: `0644` or `0755` for files, `0755` for directories. |

Select a descendant of a directory with one bracket containing its complete
relative path:

```
asset.source['main.go']
asset.source['internal/helpers.go'].content
asset.source['internal'].path
```

The bracket value must be a string literal using `/`. It cannot be empty,
absolute, computed, or contain empty, `.` or `..` segments. Use one bracket for
the complete path; `asset.source['internal']['helpers.go']` is invalid. A file
asset does not support descendant selection.

Asset paths can be passed to Go string inputs and asset content can be passed to
Go `[]byte` inputs, including values nested in lists, maps, and objects. Paths
remain logical until a Go call needs an operating-system path. Content works
with byte-compatible core functions, for example:

```
locals: {
  archive-base64: @core.b64-encode(asset.archive.content)
  same-content:   asset.archive.content == asset.archive.content
}
```

`==` and `!=` accept asset paths where strings are accepted and compare asset
content with other byte values. Path comparison uses the logical reference, not
a cache path. Comparing bytes with a string is a type mismatch. Asset values
cannot be interpolated, and asset content cannot be returned directly as a
factory output.

Asset names are local to the body that declares them. A composite reads its own
assets, does not inherit the caller's asset names, and does not expose its asset
names to callers. A path or content value can still pass through a declared
composite input before reaching Go code.

### Asset permissions

Unobin records portable Unix modes:

| Source entry | Recorded mode | Runtime cache mode |
| --- | --- | --- |
| Directory | `0755` | `0555` |
| Regular file with an execute bit | `0755` | `0555` |
| Other regular file | `0644` | `0444` |

Setuid, setgid, and sticky bits are rejected. Ownership, ACLs, extended
attributes, and timestamps are not included. A permission change affects asset
identity even when the file bytes do not change.

### Asset cache

Compiled factories materialize paths and retain resolved content in a
content-addressed cache. Use the persistent root flag to choose its directory:

```
./build/appdeploy --asset-cache-dir ./asset-cache plan -c dev.ub -o plan.ubp
./build/appdeploy --asset-cache-dir ./apply-cache apply plan.ubp
```

Without the flag, Unobin uses the platform user cache directory under
`unobin/assets`. Relative flag values are resolved from the current working
directory. The cache is created only when a command resolves an asset, so
metadata-only commands do not create it.

Current asset values can be reconstructed from the embedded factory bundle, so
a saved plan can be applied with a different or initially empty cache. An older
asset identity that is no longer in the current binary can be resolved only
when its data remains in the selected cache. If it is absent, Unobin reports the
logical asset reference and asks for the cache from the earlier factory run; it
never substitutes current content.

Go library read and delete operations should therefore prefer persisted
external identifiers and outputs over reopening deployment source from an older
factory revision.

## Input constraints

Use `constraints:` to reject invalid combinations of factory inputs when a stack is planned. The compiler checks constraint syntax and verifies that `fields:` entries name declared inputs. The compiled factory evaluates the rules after stack inputs and defaults are combined, before it plans resources.

```
constraints: [
  { kind: exactly-one-of, fields: [input.image, input.build] },
  { kind: required-with, fields: [input.image, input.registry] },
  { kind: required-together, fields: [input.replicas[*].cert, input.replicas[*].key] },
  {
    kind:    predicate
    when:    input.tier == 'prod'
    require: @core.length(input.replicas) >= 2
    message: 'prod needs at least two replicas'
  },
  {
    kind:      predicate
    @for-each: input.replicas
    when:      @each.value.tls == true
    require:   @each.value.cert != null
    message:   'tls replicas need certs'
  },
]
```

Field-based constraints use a `fields:` list of input references. The set kinds are `exactly-one-of`, `at-least-one-of`, `at-most-one-of`, `required-together`, `required-with`, and `forbidden-with`. A field can point into an object or list item, such as `input.code.inline` or `input.listeners[0].cert`. A `[*]` field checks each list element; all splatted fields in one set rule must refer to the same list.

Predicate constraints evaluate `when:` first. If it is false, the rule passes. If it is true, `require:` must be true. A `message:` value replaces the default failure text.

A predicate can iterate over a list or map:

```
{
  kind:      predicate
  @for-each: input.replicas
  when:      @each.value.tls == true
  require:   @each.value.cert != null
}
```

If the iterable may be null, use an explicit fallback:

```
{
  kind:      predicate
  @for-each: input.replicas ?? []
  when:      @each.value.tls == true
  require:   @each.value.cert != null
}
```

Use `?? {}` for an optional map. A bare optional list or map is rejected because
`@each` needs a non-null iterable.

For nested iteration, use a list of binding objects:

```
{
  kind: predicate
  @for-each: [
    { @rule:       input.rules },
    { @transition: @rule.value.transitions },
  ]
  when:    true
  require: @transition.value.days != null
}
```

Factory constraints may read `input.*` values only. They may call `@core` functions and imported Go functions. Use them for the factory's input contract; Go library constraints still check each resource, data source, or action body wherever that library kind is called.

## Resources, data sources, and actions

Resource, data source, and action bodies call imported library kinds:

```
actions: {
  read-back: std.exec-command {
    @trigger: resource.hello.sha256
    argv:     ['cat', input.path]
  }
}

data-sources: {
  github: aws.iam-openid-connect-provider {
    url: input.oidc-provider-url
  }
}

resources: {
  app: std.fs-file {
    path: local.path
    content: input.content
  }
}
```

## State moves

State moves rename entries in state without recreating the external object:

```
state-moves: [
  { from: resource.old, to: resource.app },
]
```

## Library configs

If an imported Go library declares a configuration schema, bind the import alias to the configuration in `library-configs`:

```
inputs: {
  cloud-config: {
    @sensitive: true
    type:       library-config('github.com/example/cloud')
    default:    {}
  }
}

imports: { cloud: 'github.com/example/cloud' }

library-configs: {
  cloud: input.cloud-config
}
```

Inputs may use `library-config('...')` to define the configuration type, passing in the library path. Thus, ordinary inputs are used to configure libraries; there is no separate configuration mechanism like Terraform uses for providers.

The `library-config(...)` path may name a separate configuration package. This
is useful when one repository has service packages that share a config package:

```
inputs: {
  aws: { type: library-config('example.com/aws//config') }
}

imports: {
  s3: 'example.com/aws//s3'
}

library-configs: {
  s3: input.aws
}
```

The imported service package can set `Library().Configuration` to
`config.LibraryConfiguration()`. The config package must provide
`LibraryConfiguration()`.

To configure multiple library instances with different configurations, import the
library again under a different alias, and bind the configurations separately:

```
inputs: {
  cloud-config: {
    @sensitive: true
    type:       library-config('github.com/example/cloud')
    default:    {}
  }
}

locals: {
  cloud-config-east: @core.merge(input.cloud-config, { region: 'east' })
}

imports: {
  cloud:      'github.com/example/cloud'
  cloud-east: 'github.com/example/cloud'
}

library-configs: {
  cloud:      input.cloud-config
  cloud-east: local.cloud-config-east
}
```

## Compiling

Compile from the source root:

```
unobin compile -o ./build --build --library-path github.com/example/appdeploy
```

Alternatively, pass the path to the source root:

```
unobin compile -o ./build --build --library-path github.com/example/appdeploy -p ./factory-abc
```

The resulting executable is the factory.
