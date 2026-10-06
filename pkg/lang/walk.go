package lang

// Walk invokes visit for e and then for every nested expression in
// source order. It recurses into object field values, array elements,
// call args, infix and prefix operands, dot-path index expressions,
// conditional branches, comprehension parts, parsed type declarations,
// and interpolated-string slots. A nil expression is a no-op so callers
// can recurse through optional fields without guarding first.
func Walk(e Expr, visit func(Expr)) {
	walkExpr(e, visit, nil)
}

func walkExpr(e Expr, visit func(Expr), scanner *exprScanner) {
	if e == nil {
		return
	}
	if scanner == nil {
		visit(e)
	} else if scanner.stopped || scanner.visit(e) == ScanSkipChildren || scanner.stopped {
		return
	}
	switch v := e.(type) {
	case *ObjectLit:
		for _, fld := range v.Fields {
			if fld.Decl != nil {
				walkExpr(fld.Decl.Body, visit, scanner)
				continue
			}
			walkExpr(fld.Value, visit, scanner)
		}
	case *ArrayLit:
		for _, el := range v.Elements {
			walkExpr(el, visit, scanner)
		}
	case *Call:
		for _, a := range v.Args {
			walkExpr(a, visit, scanner)
		}
	case *Infix:
		walkExpr(v.Left, visit, scanner)
		walkExpr(v.Right, visit, scanner)
	case *Prefix:
		walkExpr(v.Expr, visit, scanner)
	case *DotPath:
		for _, seg := range v.Segments {
			walkExpr(seg.Index, visit, scanner)
		}
	case *Conditional:
		walkExpr(v.Cond, visit, scanner)
		walkExpr(v.Then, visit, scanner)
		walkExpr(v.Else, visit, scanner)
	case *Comprehension:
		walkExpr(v.Source, visit, scanner)
		base := 0
		if scanner != nil {
			base = len(scanner.bindings)
			scanner.bindings = append(scanner.bindings, v.Names...)
		}
		walkExpr(v.Key, visit, scanner)
		walkExpr(v.Value, visit, scanner)
		walkExpr(v.Filter, visit, scanner)
		if scanner != nil {
			scanner.bindings = scanner.bindings[:base]
		}
	case *InterpolatedString:
		for _, part := range v.Parts {
			walkExpr(part.Expr, visit, scanner)
		}
	case *TypeList:
		walkExpr(v.Elem, visit, scanner)
	case *TypeMap:
		walkExpr(v.Elem, visit, scanner)
	case *TypeObject:
		for _, field := range v.Fields {
			if field.Type != nil {
				walkExpr(field.Type, visit, scanner)
			}
			if field.Decl != nil {
				walkExpr(field.Decl, visit, scanner)
			}
		}
	case *TypeTuple:
		for _, elem := range v.Elements {
			walkExpr(elem, visit, scanner)
		}
	case *TypeOptional:
		walkExpr(v.Elem, visit, scanner)
	}
}
