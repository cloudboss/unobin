# If expressions

An if expression chooses between two values:

```
if input.environment == 'prod' then 3 else 1
```

Both branches must evaluate to the same type. If the branches have different concrete types, the checker reports a mismatch unless a common type is valid for that position.

For optional types, null checks cause values to be narrowed in the branch they guard. In the example below, `input.greeting` begins as `optional(string)` but is a plain `string` in the `else` branch.

```
banner: if input.greeting == null then $'Hello, {{ input.name }}' else input.greeting
```

For a simple fallback, prefer the `??` operator:

```
input.tls?.cert ?? 'self-signed'
```
