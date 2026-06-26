package lang

// ScanContext describes the lexical scope at the expression currently visited.
type ScanContext struct {
	// ComprehensionBindings names comprehension variables visible at this expression.
	// Callbacks must treat the slice as read-only.
	ComprehensionBindings []string
}

// ScanCallbacks receives pre-order expression visits from ScanExpr.
type ScanCallbacks struct {
	Expr    func(Expr, ScanContext) ScanDecision
	DotPath func(*DotPath, ScanContext) ScanDecision
	Call    func(*Call, ScanContext) ScanDecision
}

// ScanDecision controls whether ScanExpr keeps visiting nested expressions.
type ScanDecision int

const (
	ScanContinue ScanDecision = iota
	ScanSkipChildren
	ScanStop
)

// ScanExpr visits expr and nested expressions in source order. It extends Walk
// with scoped callbacks for dotted paths, calls, early stop, and child skipping.
func ScanExpr(expr Expr, callbacks ScanCallbacks) {
	s := exprScanner{callbacks: callbacks}
	s.scan(expr)
}

type exprScanner struct {
	callbacks ScanCallbacks
	bindings  []string
	stopped   bool
}

func (s *exprScanner) scan(expr Expr) {
	if expr == nil || s.stopped {
		return
	}
	if s.visit(expr) == ScanSkipChildren || s.stopped {
		return
	}
	s.scanChildren(expr)
}

func (s *exprScanner) visit(expr Expr) ScanDecision {
	decision := ScanContinue
	ctx := ScanContext{ComprehensionBindings: s.bindings}
	if s.callbacks.Expr != nil {
		decision = mergeScanDecision(decision, s.callbacks.Expr(expr, ctx))
	}
	switch v := expr.(type) {
	case *DotPath:
		if s.callbacks.DotPath != nil {
			decision = mergeScanDecision(decision, s.callbacks.DotPath(v, ctx))
		}
	case *Call:
		if s.callbacks.Call != nil {
			decision = mergeScanDecision(decision, s.callbacks.Call(v, ctx))
		}
	}
	if decision == ScanStop {
		s.stopped = true
	}
	return decision
}

func mergeScanDecision(a, b ScanDecision) ScanDecision {
	if a == ScanStop || b == ScanStop {
		return ScanStop
	}
	if a == ScanSkipChildren || b == ScanSkipChildren {
		return ScanSkipChildren
	}
	return ScanContinue
}

func (s *exprScanner) scanChildren(expr Expr) {
	switch v := expr.(type) {
	case *ObjectLit:
		for _, fld := range v.Fields {
			if fld.Decl != nil {
				s.scan(fld.Decl.Body)
				continue
			}
			s.scan(fld.Value)
		}
	case *ArrayLit:
		for _, el := range v.Elements {
			s.scan(el)
		}
	case *Call:
		for _, arg := range v.Args {
			s.scan(arg)
		}
	case *Infix:
		s.scan(v.Left)
		s.scan(v.Right)
	case *Prefix:
		s.scan(v.Expr)
	case *DotPath:
		for _, seg := range v.Segments {
			s.scan(seg.Index)
		}
	case *Conditional:
		s.scan(v.Cond)
		s.scan(v.Then)
		s.scan(v.Else)
	case *Comprehension:
		s.scan(v.Source)
		base := len(s.bindings)
		s.bindings = append(s.bindings, v.Names...)
		s.scan(v.Key)
		s.scan(v.Value)
		s.scan(v.Filter)
		s.bindings = s.bindings[:base]
	case *InterpolatedString:
		for _, part := range v.Parts {
			s.scan(part.Expr)
		}
	case *TypeList:
		s.scan(v.Elem)
	case *TypeMap:
		s.scan(v.Elem)
	case *TypeObject:
		for _, field := range v.Fields {
			s.scan(field.Type)
			s.scan(field.Decl)
		}
	case *TypeTuple:
		for _, elem := range v.Elements {
			s.scan(elem)
		}
	case *TypeOptional:
		s.scan(v.Elem)
	}
}
