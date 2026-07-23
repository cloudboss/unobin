# Language Design Review: Asset Value Operations

## Executive summary

Unobin is a declarative configuration language whose asset values must stay
deterministic while remaining useful in ordinary expressions. Asset paths
should use the language's existing string equality, and asset content should
use byte-for-byte equality. `@core.b64-encode` should accept `string | bytes`;
asset content then works through the same bytes contract as every other bytes
value. Runtime conversion must follow semantic types so an asset content
reference is resolved to bytes instead of encoding its internal reference.

## The brief

The language is written by factory authors and evaluated while planning and
applying infrastructure. The relevant constraints are deterministic plans,
portable saved plans, precise static errors, Go interoperability, and an
implementation that can be maintained by a small team. For asset operations,
the priority order is predictable semantics, consistency with existing
operators and functions, portability, and implementation simplicity.

## Equality for asset values

### Alternatives

#### Reject equality

```ub
local.same-path == asset.source.path
local.same-content == asset.archive.content
```

Both expressions would be type errors.

#### Compare internal references

```ub
asset.first.content == asset.second.content
```

This compares the internal reference text. Equal bytes declared under
different asset names could compare unequal.

#### Compare semantic values

```ub
asset.source.path == local.expected-path
asset.first.content == asset.second.content
asset.first.content != asset.third.content
```

Paths compare as strings. Content compares byte for byte.

### Tradeoffs

Rejecting equality is easy to implement, but it makes two stable value types
less regular than strings and other collections. Comparing internal
references is deterministic, but reference metadata such as an asset name can
affect the result even when the represented bytes are equal. Semantic
comparison matches the types authors see, but content references must be
resolved before comparison.

### Recommendation

Use semantic equality. Permit `==` and `!=` for asset paths wherever string
equality is valid, and permit them for bytes. Keep arithmetic, ordering, and
boolean operators invalid because neither paths nor bytes have useful numeric,
ordered, or truth-value semantics.

## Base64 encoding

### Alternatives

#### Accept strings only

```ub
@core.b64-encode('hello')
```

Asset content must first pass through some unrelated conversion.

#### Add an asset-content exception

```ub
@core.b64-encode(asset.archive.content)
```

The checker and evaluator identify this one expression origin specially.

#### Accept strings and bytes

```ub
@core.b64-encode('hello')
@core.b64-encode(asset.archive.content)
@core.b64-encode(local.generated-bytes)
```

The function has the semantic type `string | bytes -> string`.

### Tradeoffs

The string-only form preserves current behavior but excludes the natural
binary input. An asset-specific exception produces surprising differences
between equal byte values based on their origin. A string-or-bytes function
adds one overload to the builtin signature and requires runtime conversion of
byte inputs, but gives one rule for all byte values.

### Recommendation

Define `@core.b64-encode` as `string | bytes -> string`. Its implementation
should receive either ordinary string bytes or a byte sequence and encode that
sequence. Asset resolution belongs in the generic semantic-value conversion,
not in the function.

## Resolving internal asset references

### Alternatives

#### Compare and encode reference text

```ub
@core.b64-encode(asset.archive.content)
```

This would encode the internal reference rather than the archive bytes.

#### Resolve every asset reference immediately

```ub
locals: {
  content: asset.archive.content
}
```

Evaluating the local would immediately allocate the complete byte sequence.

#### Resolve according to the consuming type

```ub
locals: {
  content: asset.archive.content
  encoded: @core.b64-encode(local.content)
}
```

The local retains its internal bytes reference. Equality or a bytes parameter
requests its byte value when needed.

### Tradeoffs

Treating reference text as the value is inexpensive but semantically wrong.
Immediate resolution is simple to explain but reads and allocates content that
may only be passed through. Consumer-directed resolution preserves lazy asset
behavior and makes Go and builtin parameters follow the same rule, at the cost
of retaining semantic type information until the operation.

### Recommendation

Resolve asset references according to the operation's semantic input type.
Use the same resolver for builtin functions, imported functions, resource
inputs, configuration inputs, and equality. Do not inspect the source
expression to decide whether bytes are accepted.

## Recommendations summary

| Recommendation | Dimension | Impact | Effort or risk |
| --- | --- | --- | --- |
| Compare asset paths as strings | Semantics | High | Low |
| Compare content byte for byte | Semantics | High | Medium |
| Type `b64-encode` as `string | bytes -> string` | Type system | High | Medium |
| Resolve values from the consuming semantic type | Implementation | High | Medium |
| Keep arithmetic, ordering, and boolean use invalid | Type system | Medium | Low |

## Open questions

- Whether interpolation should accept asset paths as strings or continue to
  require an explicit conversion.
- Whether `@core.to-json` should encode bytes as a base64 string or reject
  bytes until the language has a documented JSON bytes representation.
- Whether byte equality failures caused by a corrupt embedded bundle should
  point at the operator or at the asset reference.
