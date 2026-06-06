package typecheck

import (
	"maps"
	"strings"

	"github.com/cloudboss/unobin/pkg/lang"
)

// NarrowedWhere returns scope with the narrowings a true condition
// puts in force, or the scope itself when the condition proves
// nothing. A constraint's require is checked this way under its when.
func NarrowedWhere(scope *Scope, cond lang.Expr) *Scope {
	facts, _ := nullFacts(cond, scope)
	return scope.narrowed(facts)
}

// narrowed returns a child scope with facts overlaid on any narrowing
// already in force. A nil or empty fact set returns the scope itself.
func (s *Scope) narrowed(facts map[string]Type) *Scope {
	if s == nil || len(facts) == 0 {
		return s
	}
	child := *s
	m := make(map[string]Type, len(s.Narrowed)+len(facts))
	maps.Copy(m, s.Narrowed)
	maps.Copy(m, facts)
	child.Narrowed = m
	return &child
}

// nullFacts derives the reference paths a condition proves null or
// non-null: whenTrue holds the narrowings in force where cond held,
// whenFalse where it did not. The condition forms that produce facts
// are `path == null`, `path != null`, their `!` negations, and `&&`
// conjunctions, mirroring the evaluator's short circuit: everything
// else proves nothing. Only exact, index-free paths narrow.
func nullFacts(cond lang.Expr, scope *Scope) (whenTrue, whenFalse map[string]Type) {
	switch v := cond.(type) {
	case *lang.Prefix:
		if v.Op == "!" {
			t, f := nullFacts(v.Expr, scope)
			return f, t
		}
	case *lang.Infix:
		switch v.Op {
		case "&&":
			lt, _ := nullFacts(v.Left, scope)
			rt, _ := nullFacts(v.Right, scope)
			return mergeFacts(lt, rt), nil
		case "==", "!=":
			path := nullComparedPath(v)
			if path == nil {
				return nil, nil
			}
			key, ok := narrowKey(path)
			if !ok {
				return nil, nil
			}
			inner := Infer(path, TUnknown(), scope, lang.NewErrorList(0)).Unwrap()
			isNull := map[string]Type{key: TNull()}
			var notNull map[string]Type
			if inner.IsKnown() {
				notNull = map[string]Type{key: inner}
			}
			if v.Op == "==" {
				return isNull, notNull
			}
			return notNull, isNull
		}
	}
	return nil, nil
}

// nullComparedPath returns the reference side of a comparison against
// null, or nil when the comparison is not a null test.
func nullComparedPath(in *lang.Infix) lang.Expr {
	if _, ok := in.Right.(*lang.NullLit); ok {
		return pathExpr(in.Left)
	}
	if _, ok := in.Left.(*lang.NullLit); ok {
		return pathExpr(in.Right)
	}
	return nil
}

func pathExpr(e lang.Expr) lang.Expr {
	switch e.(type) {
	case *lang.DotPath, *lang.Ident:
		return e
	}
	return nil
}

// narrowKey renders a reference as the canonical key narrowing is
// recorded under: the root plus its named segments. A path with an
// index, splat, or unnamed segment has no key, since a null test on
// one element says nothing about another evaluation of the index.
func narrowKey(e lang.Expr) (string, bool) {
	switch v := e.(type) {
	case *lang.Ident:
		return v.Name, true
	case *lang.DotPath:
		if v.Root == nil {
			return "", false
		}
		parts := []string{v.Root.Name}
		for _, seg := range v.Segments {
			if seg.Splat || seg.Index != nil || seg.Name == "" {
				return "", false
			}
			parts = append(parts, seg.Name)
		}
		return strings.Join(parts, "."), true
	}
	return "", false
}

// narrowedLookup finds the longest narrowed prefix of a dot path and
// returns the narrowed type with the segments remaining past the
// prefix. Prefixes run through named segments only.
func narrowedLookup(scope *Scope, dp *lang.DotPath) (Type, []lang.DotSegment, bool) {
	if scope == nil || len(scope.Narrowed) == 0 || dp.Root == nil {
		return Type{}, nil, false
	}
	keys := []string{dp.Root.Name}
	consumed := []int{0}
	var key strings.Builder
	key.WriteString(dp.Root.Name)
	for i, seg := range dp.Segments {
		if seg.Splat || seg.Index != nil || seg.Name == "" {
			break
		}
		key.WriteString("." + seg.Name)
		keys = append(keys, key.String())
		consumed = append(consumed, i+1)
	}
	for j := len(keys) - 1; j >= 0; j-- {
		if t, ok := scope.Narrowed[keys[j]]; ok {
			return t, dp.Segments[consumed[j]:], true
		}
	}
	return Type{}, nil, false
}

func mergeFacts(a, b map[string]Type) map[string]Type {
	if len(a) == 0 {
		return b
	}
	if len(b) == 0 {
		return a
	}
	out := make(map[string]Type, len(a)+len(b))
	maps.Copy(out, a)
	maps.Copy(out, b)
	return out
}
