# First factory

Start by generating a factory source directory:

```
unobin generate factory -o appdeploy
cd appdeploy
```

The generated directory contains:

- `factory.ub`, the factory source file.
- `project.ub`, the dependency project file.

Edit `factory.ub` into a small file-writing factory:

```
factory: {
  description: 'Writes a greeting file.'

  inputs: {
    message: { type: string, description: 'Text to write' }
    path:    { type: string, description: 'File path to write' }
  }

  imports: { std: 'github.com/cloudboss/unobin-library-std' }

  resources: {
    greeting: std.fs-file {
      path:    input.path
      content: input.message
    }
  }

  outputs: {
    path: { value: resource.greeting.path }
    size: { value: resource.greeting.size }
  }
}
```

Add the imported library to the dependency project and write the lock:

```
unobin deps get github.com/cloudboss/unobin-library-std@v0.2.1
```

You can now compile the factory.
