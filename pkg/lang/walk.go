package lang

// Walk invokes visit for e and then for every nested expression in
// source order. It recurses into object field values, array elements,
// call args, infix and prefix operands, dot-path index expressions,
// conditional branches, and comprehension parts. A nil expression is a
// no-op so callers can recurse through optional fields without guarding
// first.
func Walk(e Expr, visit func(Expr)) {
	if e == nil {
		return
	}
	visit(e)
	switch v := e.(type) {
	case *ObjectLit:
		for _, fld := range v.Fields {
			Walk(fld.Value, visit)
		}
	case *ArrayLit:
		for _, el := range v.Elements {
			Walk(el, visit)
		}
	case *Call:
		for _, a := range v.Args {
			Walk(a, visit)
		}
	case *Infix:
		Walk(v.Left, visit)
		Walk(v.Right, visit)
	case *Prefix:
		Walk(v.Expr, visit)
	case *DotPath:
		for _, seg := range v.Segments {
			Walk(seg.Index, visit)
		}
	case *Conditional:
		Walk(v.Cond, visit)
		Walk(v.Then, visit)
		Walk(v.Else, visit)
	case *Comprehension:
		Walk(v.Source, visit)
		Walk(v.Key, visit)
		Walk(v.Value, visit)
		Walk(v.Filter, visit)
	}
}
