package lang

// OutputValueExpr returns the inner expression of an output entry's
// wrapper. Every output in an `outputs:` block is of the form
// `name: { value: expr }`; this helper unwraps to the inner expr.
// Returns nil when the input is not a wrapper or has no `value:`
// key (treat as a structural error caught by ValidateOutputs).
func OutputValueExpr(e Expr) Expr {
	obj, ok := e.(*ObjectLit)
	if !ok {
		return nil
	}
	for _, df := range obj.Fields {
		if df.Key.Kind == FieldIdent && !df.Key.IsMeta() && df.Key.Name == "value" {
			return df.Value
		}
	}
	return nil
}
