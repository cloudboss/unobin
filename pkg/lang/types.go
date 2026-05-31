package lang

// PromoteType interprets e as a type expression and returns the corresponding
// TypeExpr tree. Type expressions are syntactically a subset of plain
// expressions: a bare identifier names an atomic type; a bare call names a
// constructor (list, set, map, tuple, object, optional). Anything outside
// that subset is rejected with an ErrType diagnostic.
//
// Default values inside optional(T, default) are not promoted; they remain
// plain Expr because TypeOptional.Default is a value, not a type.
func PromoteType(e Expr) (TypeExpr, error) {
	switch v := e.(type) {
	case *Ident:
		return promoteAtomic(v)
	case *NullLit:
		// `null` is a token (NullLit), not an Ident; at type position it
		// names the null atomic type.
		return &TypeAtomic{S: v.S, Name: "null"}, nil
	case *Call:
		return promoteCall(v)
	default:
		return nil, Errorf(ErrType, exprPos(e), "expected type expression, got %s", exprKind(e))
	}
}

var atomicTypeNames = map[string]struct{}{
	"string":  {},
	"number":  {},
	"integer": {},
	"boolean": {},
	"null":    {},
	"any":     {},
}

func promoteAtomic(id *Ident) (TypeExpr, error) {
	if _, ok := atomicTypeNames[id.Name]; !ok {
		return nil, Errorf(ErrType, id.S.Start,
			"unknown atomic type %q (expected string, number, integer, boolean, null, any)", id.Name)
	}
	return &TypeAtomic{S: id.S, Name: id.Name}, nil
}

// typeConstructorNames are the call-form type constructors. promoteCall
// below dispatches each one; keep the two in sync when adding a constructor.
var typeConstructorNames = map[string]struct{}{
	"list":     {},
	"set":      {},
	"map":      {},
	"tuple":    {},
	"object":   {},
	"optional": {},
}

// isTypeConstructor reports whether name is a call-form type constructor
// rather than a library function. The call checker uses it to tell a type
// like list(string) from a function call.
func isTypeConstructor(name string) bool {
	_, ok := typeConstructorNames[name]
	return ok
}

func promoteCall(c *Call) (TypeExpr, error) {
	if c.Library != nil {
		return nil, Errorf(ErrType, c.S.Start,
			"library-qualified call %s.%s is not a type expression", c.Library.Name, c.Func.Name)
	}
	name := c.Callee.Name
	switch name {
	case "list":
		return promoteContainer(c, name, func(t TypeExpr) TypeExpr { return &TypeList{S: c.S, Elem: t} })
	case "set":
		return promoteContainer(c, name, func(t TypeExpr) TypeExpr { return &TypeSet{S: c.S, Elem: t} })
	case "map":
		return promoteContainer(c, name, func(t TypeExpr) TypeExpr { return &TypeMap{S: c.S, Elem: t} })
	case "tuple":
		return promoteTuple(c)
	case "object":
		return promoteObject(c)
	case "optional":
		return promoteOptional(c)
	default:
		return nil, Errorf(ErrType, c.S.Start, "unknown type constructor %q", name)
	}
}

func promoteContainer(c *Call, name string, build func(TypeExpr) TypeExpr) (TypeExpr, error) {
	if len(c.Args) != 1 {
		return nil, Errorf(ErrType, c.S.Start,
			"%s takes exactly 1 type argument, got %d", name, len(c.Args))
	}
	elem, err := PromoteType(c.Args[0])
	if err != nil {
		return nil, err
	}
	return build(elem), nil
}

func promoteTuple(c *Call) (TypeExpr, error) {
	if len(c.Args) != 1 {
		return nil, Errorf(ErrType, c.S.Start,
			"tuple takes exactly 1 array argument of types, got %d", len(c.Args))
	}
	arr, ok := c.Args[0].(*ArrayLit)
	if !ok {
		return nil, Errorf(ErrType, exprPos(c.Args[0]),
			"tuple expects an array literal of types, got %s", exprKind(c.Args[0]))
	}
	elems := make([]TypeExpr, len(arr.Elements))
	for i, e := range arr.Elements {
		t, err := PromoteType(e)
		if err != nil {
			return nil, err
		}
		elems[i] = t
	}
	return &TypeTuple{S: c.S, Elements: elems}, nil
}

func promoteObject(c *Call) (TypeExpr, error) {
	if len(c.Args) != 1 {
		return nil, Errorf(ErrType, c.S.Start,
			"object takes exactly 1 object literal of fields, got %d arguments", len(c.Args))
	}
	obj, ok := c.Args[0].(*ObjectLit)
	if !ok {
		return nil, Errorf(ErrType, exprPos(c.Args[0]),
			"object expects an object literal of fields, got %s", exprKind(c.Args[0]))
	}
	fields := make([]*TypeObjectField, 0, len(obj.Fields))
	for _, f := range obj.Fields {
		if f.Key.Kind != FieldIdent {
			return nil, Errorf(ErrType, f.Key.S.Start,
				"object field name must be a bare identifier")
		}
		of, err := promoteObjectField(f)
		if err != nil {
			return nil, err
		}
		fields = append(fields, of)
	}
	return &TypeObject{S: c.S, Fields: fields}, nil
}

func promoteObjectField(f *Field) (*TypeObjectField, error) {
	// An object-literal value is an input declaration when it carries a
	// `type:` key (alongside any modifier keys); leave its promotion to the
	// schema validator. An object literal without `type:` is malformed in
	// type position - any other shape recurses through PromoteType.
	if obj, ok := f.Value.(*ObjectLit); ok {
		if hasTypeKey(obj) {
			return &TypeObjectField{S: f.S, Name: f.Key.Name, Decl: obj}, nil
		}
		return nil, Errorf(ErrType, obj.S.Start,
			"field %q: object literal without 'type:' is not a valid type expression", f.Key.Name)
	}
	t, err := PromoteType(f.Value)
	if err != nil {
		return nil, err
	}
	return &TypeObjectField{S: f.S, Name: f.Key.Name, Type: t}, nil
}

func hasTypeKey(o *ObjectLit) bool {
	for _, f := range o.Fields {
		if f.Key.Kind == FieldIdent && f.Key.Name == "type" {
			return true
		}
	}
	return false
}

func promoteOptional(c *Call) (TypeExpr, error) {
	if len(c.Args) < 1 || len(c.Args) > 2 {
		return nil, Errorf(ErrType, c.S.Start,
			"optional takes 1 or 2 arguments (type, [default]), got %d", len(c.Args))
	}
	elem, err := PromoteType(c.Args[0])
	if err != nil {
		return nil, err
	}
	var dflt Expr
	if len(c.Args) == 2 {
		dflt = c.Args[1]
	}
	return &TypeOptional{S: c.S, Elem: elem, Default: dflt}, nil
}

func exprPos(e Expr) Position {
	if e == nil {
		return Position{}
	}
	return e.Span().Start
}

func exprKind(e Expr) string {
	switch e.(type) {
	case *ObjectLit:
		return "object literal"
	case *ArrayLit:
		return "array literal"
	case *StringLit:
		return "string literal"
	case *NumberLit:
		return "number literal"
	case *BoolLit:
		return "boolean literal"
	case *NullLit:
		return "null literal"
	case *Ident:
		return "identifier"
	case *DotPath:
		return "dot-path"
	case *Call:
		return "call"
	case *Infix, *Prefix:
		return "operator expression"
	default:
		return "expression"
	}
}
