# Stack files

One factory can manage multiple instances of resources, called stacks. One stack corresponds to one location in the state backend. A stack file is the input for one stack. It defines state storage, encryption settings, and factory input values. It cannot be reused by multiple stacks.

The encryption block applies to both state and plan outputs.

Generate a starter file from a compiled factory:

```
./appdeploy schema template -o dev.ub
```

A stack file contains one `stack` declaration:

```
stack: {
  locals: {
    aws-region: 'us-west-2'
    aws-config: { region: local.aws-region }
    kms-key-id: $'arn:aws:kms:{{ local.aws-region }}:012345678901:key/04fbed55-3830-4bf2-9c28-644aab645311'
  }

  factory: {
    inputs: {
      name:       'dev'
      aws-config: local.aws-config
    }
  }

  state: s3 {
    bucket: '.unobin/state'
    prefix: '.unobin/state'
    aws:    local.aws-config
  }

  encryption: kms {
    key-id: local.kms-key-id
    aws:    local.aws-config
  }
}
```

A `locals` block in a stack file is the top level scope. It can define variables that are reused among state, encryption, and factory inputs.

## Input environment variables

Factory inputs can be passed in as environment variables named `UB_INPUT_<name>`, where `<name>` is in snake case and converted to kebab case. Inputs defined in stack files take priority, so environment variables take effect only for inputs that are omitted from the stack file.

Environment values are decoded against the declared input type. String inputs use the raw text, so if `input.abc` is defined as a `string`, `UB_INPUT_abc=true` is the string `true`, not decoded to a boolean value. Other types first try Unobin literals such as `true`, `5`, or `['a', 'b']`, falling back to JSON. If the decoded value does not match the declared type, the command fails.

The following are valid possibilities:

```
UB_INPUT_az_map="{'us-east-1a': 'subnet-9bcba3cf2f71bf934', 'us-east-1b': 'subnet-edb4ca868a33e5daf'}"
UB_INPUT_az_map='{"us-east-1a": "subnet-9bcba3cf2f71bf934", "us-east-1b": "subnet-edb4ca868a33e5daf"}'
UB_INPUT_azs="['us-east-1a', 'us-east-1b', 'us-east-1c']"
UB_INPUT_azs='["us-east-1a", "us-east-1b", "us-east-1c"]'
```

## Factory pin

Generated stack schema templates include a factory pin. This records the factory's library path, version, and content revision accepted by this stack file.

```
factory: {
  pin: {
    library-path: 'github.com/example/appdeploy'
    supported-versions: [
      { version: 'v1.2.2', content-revision: 'abc123' },
    ]
  }
  inputs: { ... }
}
```

After compiling a new version of a factory, pin it to the stack file with the `pin` subcommand:

```
./appdeploy pin -c dev.ub
Pinned v1.2.3 (content-revision 171751aa9227) in dev.ub (appended entry).
```

Plan, refresh, and validate check the pin before using the stack file.

## State

`state: local` stores state under a local directory:

```
state: local {
  path: '.unobin/state'
}
```

`state: s3` stores state in S3:

```
state: s3 {
  bucket: 'acme-unobin-state'
  prefix: 'appdeploy/dev'
}
```

`state: gcs` stores state in Google Cloud Storage:

```
state: gcs {
  bucket:       'acme-unobin-state'
  prefix:       'appdeploy/dev'
  kms-key-name: 'projects/acme/locations/us/keyRings/state/cryptoKeys/storage'
  gcp:          { project: 'acme-prod' }
}
```

`kms-key-name` is the GCS CMEK setting for stored objects. It is separate from
an envelope encrypter's `key-id`.

### Bucket bootstrap

S3 and GCS support an optional `bootstrap` object. A nonempty object enables
creation of a missing bucket. Omit it, use `null`, or use `{}` to require an
existing bucket. `bootstrap` does not accept a boolean and is not available
for the local backend. Explicit `false` values inside the object still enable
bootstrap.

An existing bucket is used as-is. Unobin does not compare or change its
configuration. When S3 reports that the bucket is already owned by the
account, or GCS reports a creation conflict, Unobin checks access and skips
the creation options. Other S3 name collisions return an error. Access errors
stop initialization; they do not trigger creation.

Bootstrap runs when a command opens the state backend. This includes `plan`
and state inspection commands, so those commands can create a bucket when
bootstrap is enabled. `validate` checks configuration without cloud requests.
The bucket is separate from factory resources: Unobin does not track its
configuration in state or delete it during `destroy`.

For S3, use the same nested option names as the AWS library's S3 bucket:

```
state: s3 {
  bucket: 'acme-unobin-state'
  aws: { region: 'us-east-1' }
  bootstrap: {
    versioning: { status: 'Enabled' }
    public-access-block: {
      block-public-acls: true
      block-public-policy: true
      ignore-public-acls: true
      restrict-public-buckets: true
    }
    ownership-controls: { object-ownership: 'BucketOwnerEnforced' }
    tags: { purpose: 'state' }
  }
}
```

Supported S3 creation options:

| Option | Values |
| --- | --- |
| `bucket-namespace` | `'global'` or `'account-regional'`; omitted uses the global namespace |
| `versioning.status` | `'Enabled'` or `'Suspended'` |
| `public-access-block` | The four booleans above; omitted members of a supplied block are `false` |
| `ownership-controls.object-ownership` | `'BucketOwnerEnforced'`, `'BucketOwnerPreferred'`, or `'ObjectWriter'` |
| `tags` | An object with string values |

The bucket region comes from the resolved `aws` configuration. For the
account-regional namespace, `bucket` must contain the full name, including
the `-<account-id>-<region>-an` suffix. Unobin does not generate the name.
Options that are omitted keep the S3 defaults.

In the global namespace in `us-east-1`, the S3 create API can return success
for an owned bucket and reset its ACL. An existence check reduces this risk,
but cannot exclude a concurrent creation between the check and the request.
The account-regional namespace returns a conflict for an existing bucket.

S3 settings require separate API requests after creation. The credentials
need permission to check and create the bucket and to apply the supplied
settings. If a setting fails, initialization returns an error and retains
the bucket. Complete the settings manually before using it; later commands
reuse the bucket without retrying its configuration.

After first enabling S3 versioning, bootstrap waits 15 minutes before it
returns, as [recommended by S3](https://docs.aws.amazon.com/AmazonS3/latest/API/API_PutBucketVersioning.html)
before object writes. Existing buckets do not cause this wait.

For GCS, `location` is a bucket creation option under `bootstrap`, separate
from the connection settings under `gcp`:

```
state: gcs {
  bucket: 'acme-unobin-state'
  gcp: { project: 'acme-prod' }
  bootstrap: {
    location: 'US'
    versioning: { enabled: true }
    iam-configuration: {
      public-access-prevention: 'enforced'
      uniform-bucket-level-access: { enabled: true }
    }
    labels: { purpose: 'state' }
  }
}
```

Supported GCS creation options:

| Option | Values |
| --- | --- |
| `location` | A supported GCS region, dual-region, or multi-region |
| `versioning.enabled` | A boolean |
| `iam-configuration.public-access-prevention` | `'enforced'` or `'inherited'` |
| `iam-configuration.uniform-bucket-level-access.enabled` | A boolean |
| `labels` | An object with string values |

A missing GCS bucket requires both `gcp.project` and `bootstrap.location`.
They are not required by bootstrap when the bucket exists. Unobin does not
derive the bucket location from `gcp.region`: a bucket can use a region,
dual-region, or multi-region independently of other services. GCS applies
these settings in the bucket creation request. Credentials need permission
to read bucket metadata and create the bucket in the selected project.
Options that are omitted keep the GCS defaults.

Versioning retains old object versions in either backend. Configure a
bucket lifecycle policy separately if old versions need automatic removal.

## Encryption

The `env-key` encrypter reads a base64 AES-256 key from an environment variable:

```
encryption: env-key {
  env-var: 'UB_STATE_KEY'
}
```

The `kms` encrypter uses AWS KMS data keys:

```
encryption: kms {
  key-id: 'alias/unobin-state'
  aws: { ... }
}
```

The `gcp-kms` encrypter uses Google Cloud KMS to protect local data keys:

```
encryption: gcp-kms {
  key-id: 'projects/acme/locations/us/keyRings/state/cryptoKeys/envelope'
  gcp:    { project: 'acme-prod' }
}
```

`key-id` is a CryptoKey resource name, not a CryptoKeyVersion resource name.

## Library configs

Stack inputs can provide values used by `library-configs` in factory source:

```
factory: {
  inputs: {
    cloud: { region: 'us-east-1' }
  }
}
```
