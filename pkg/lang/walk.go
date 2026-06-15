package lang

// Walk invokes visit for e and then for every nested expression in
// source order. It recurses into object field values, array elements,
// call args, infix and prefix operands, dot-path index expressions,
// conditional branches, comprehension parts, parsed type declarations,
// and interpolated-string slots. A nil expression is a no-op so callers
// can recurse through optional fields without guarding first.
func Walk(e Expr, visit func(Expr)) {
	if e == nil {
		return
	}
	visit(e)
	switch v := e.(type) {
	case *ObjectLit:
		for _, fld := range v.Fields {
			if fld.Decl != nil {
				Walk(fld.Decl.Body, visit)
				continue
			}
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
	case *InterpolatedString:
		for _, part := range v.Parts {
			Walk(part.Expr, visit)
		}
	case *TypeList:
		Walk(v.Elem, visit)
	case *TypeMap:
		Walk(v.Elem, visit)
	case *TypeObject:
		for _, field := range v.Fields {
			if field.Type != nil {
				Walk(field.Type, visit)
			}
			if field.Decl != nil {
				Walk(field.Decl, visit)
			}
		}
	case *TypeTuple:
		for _, elem := range v.Elements {
			Walk(elem, visit)
		}
	case *TypeOptional:
		Walk(v.Elem, visit)
	}
}
