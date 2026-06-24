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

## Library configs

Stack inputs can provide values used by `library-configs` in factory source:

```
factory: {
  inputs: {
    cloud: { region: 'us-east-1' }
  }
}
```
