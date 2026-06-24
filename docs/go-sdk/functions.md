# Functions

Functions run inline during expression evaluation. They do not create state nodes.

Register a function with `runtime.MakeFunc`:

```go
func join(sep string, values ...string) (string, error) {
    return strings.Join(values, sep), nil
}

func Library() *runtime.Library {
    return &runtime.Library{
        Name: "text",
        Functions: map[string]runtime.FunctionType{
            "join": runtime.MakeFunc("join", "Join strings.", join),
        },
    }
}
```

Factory source calls it through the import alias:

```
text.join(',', input.first, input.second)
```

Function parameters and return values can use bool, int64, float64, string, any, slices, and string-keyed maps of those types. The function must return `(value, error)`.

During compile, Unobin reads the selected Go libraries and checks function existence, argument count, and argument types at call sites.
