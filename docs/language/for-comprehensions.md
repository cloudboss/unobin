# For comprehensions

A list comprehension maps each element of a list:

```
[ for s in input.services : s.name ]
```

Add a `when` clause to filter elements:

```
[ for s in input.services : s.name when s.public ]
```

A map comprehension uses `key => value`:

```
{ for s in input.services : s.name => s.port }
```

For maps, bind both key and value:

```
{ for name, port in input.ports : name => port }
```

Use `...` after the value to group repeated keys into lists:

```
{ for s in input.services : s.region => s.name... }
```

Comprehensions can nest:

```
[
  for r in input.regions :
  { region: r, names: [ for s in input.services : s.name when s.region == r ] }
]
```

Filters also narrow optional values. In this example, `u.port` is known to be an integer in the value expression:

```
[ for u in input.upstreams : u.port when u.port != null ]
```

A splat is a shortened form of comprehension. The following are equivalent:

```
subnets-a: resource.subnets[*].id
subnets-b: [ for s in resource.subnets : s.id ]
```
