package lang

// OutputValueExpr returns the inner expression of an output entry's
// wrapper. Every output in an `outputs:` block is of the form
// `name: { value: expr }`, with optional metadata keys alongside;
// this helper unwraps to the inner expr. Returns nil when the input
// is not a wrapper or has no `value:` key (treat as a structural
// error caught by ValidateOutputs).
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

// OutputDescription returns the wrapper's `description:` string, or
// "" when the entry has none. Validation guarantees the value is a
// string literal; anything else reads as absent here.
func OutputDescription(e Expr) string {
	obj, ok := e.(*ObjectLit)
	if !ok {
		return ""
	}
	for _, df := range obj.Fields {
		if df.Key.Kind == FieldIdent && !df.Key.IsMeta() && df.Key.Name == "description" {
			if s, ok := df.Value.(*StringLit); ok {
				return s.Value
			}
		}
	}
	return ""
}
