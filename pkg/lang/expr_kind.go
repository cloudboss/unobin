package lang

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
