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
	walkExpr(expr, nil, &s)
}

type exprScanner struct {
	callbacks ScanCallbacks
	bindings  []string
	stopped   bool
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
