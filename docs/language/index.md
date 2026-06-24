# Syntax overview

Unobin defines a configuration language with a JSON-like structure.

Philosophically, it more or less follows the [Zen of Python](https://peps.python.org/pep-0020/).

The language has objects, lists, scalar values, comments, identifiers, selectors, and function calls. Types are checked statically at compile time.

Object keys are kebab-cased identifiers or strings. Commas are optional between pairs and generally omitted when objects are multiline. Making commas optional is one case that is counter to the Zen of Python, but the removal of commas can provide clarity, so it's a tradeoff.

```
one-two: { one: 1, two: 2 }

'three four': {
  three: 3
  four:  4
}
```

Selectors differentiate the objects that follow them depending on the context.

In `state` blocks, the selector determines the backend to use. In the following example, `s3` is the selector:

```
state: s3 {
  bucket: 'cloudboss'
  prefix: 'unobin'
  aws:    local.aws-config
}
```

Selectors are also used in declarations for `action` / `actions`, `resource` / `resources`, `data-source` / `data-sources`, and `encryption`.

A factory source file compiles to an executable binary and starts with a `factory` declaration:

```
factory: {
  inputs: {}
  imports: {}
  resources: {}
  outputs: {}
}
```

A project file starts with `project:`. A lock file starts with `project-lock:`. A stack file starts with `stack:`.

Unobin library files declare *composites* of category kinds: actions, data sources, or resources. A composite is built from other category kinds; this is in contrast with primitives, which are always written in Go. Callers see a composite's inputs and outputs, while the resources, data sources, or actions within it are encapsulated inside. It is essentially a reusable factory that can be imported, as factories themselves cannot be imported.

A library composite is defined as an identifier key followed by its category selector and body:

```
cleanup: action { ... }
account-lookup: data-source { ... }
cluster: resource { ... }
```

A resource composite contains other resources, and may contain actions, data sources, and outputs.

In this example, callers access one `site` resource, but the implementation declares two resources internally:

```
site: resource {
  inputs: {
    dir:  { type: string }
    name: { type: string }
  }

  imports: { std: 'github.com/cloudboss/unobin-library-std' }

  resources: {
    config: std.fs-file {
      path:    $'{{ input.dir }}/config.txt'
      content: input.name
    }
    ready: std.fs-file {
      path:    $'{{ input.dir }}/ready.txt'
      content: resource.config.sha256
    }
  }

  outputs: {
    config-path: { value: resource.config.path }
    ready-path:  { value: resource.ready.path }
  }
}
```

A data source composite always exposes outputs and may contain internal data sources:

```
current-user: data-source {
  inputs: { name: { type: string } }

  imports: { os: 'example.com/infra/os' }

  data-sources: {
    user: os.user { name: input.name }
  }

  outputs: {
    uid:  { value: data-source.user.uid }
    home: { value: data-source.user.home }
  }
}
```

An action composite contains actions and may expose outputs:

```
notify: action {
  inputs: { message: { type: string } }

  imports: { chat: 'example.com/infra/chat' }

  actions: {
    send: chat.message { text: input.message }
  }

  outputs: {
    id: { value: action.send.id }
  }
}
```

Function calls are always qualified by their import alias, where `@core` is the alias for builtin functions.

```
imports: { net: 'example.com/unobin/net' }
locals: {
  empty: @core.to-json({})
  cidr:  net.cidr-host(input.cidr, 10)
}
```

Values are strings, numbers, booleans, null, objects, and lists:

```
{
  name: 'web'
  ports: [80, 443]
  enabled: true
  tags: { service: 'api' }
}
```

Comments are defined by `#` anywhere on a line.
