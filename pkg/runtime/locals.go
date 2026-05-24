package runtime

import (
	"fmt"

	"github.com/cloudboss/unobin/pkg/lang"
)

// localScope holds the `locals:` declarations for one evaluation scope
// (the stack body or a composite body). Locals are folded lazily: each
// is evaluated the first time something dereferences `local.<name>`, so
// a local that reads a resource output is not forced until that resource
// has produced its output. A value is memoized once it resolves; a local
// whose upstream has not run yet returns ErrEvalNotFound and is retried
// on the next dereference rather than caching the miss.
type localScope struct {
	exprs   map[string]lang.Expr
	values  map[string]any
	forcing map[string]bool
}

// newLocalScope builds a localScope from a parsed `locals:` block. A nil
// block yields a usable empty scope, so every EvalContext can carry one.
func newLocalScope(block *lang.ObjectLit) *localScope {
	ls := &localScope{
		exprs:   map[string]lang.Expr{},
		values:  map[string]any{},
		forcing: map[string]bool{},
	}
	if block == nil {
		return ls
	}
	for _, fld := range block.Fields {
		if fld.Key.Kind != lang.FieldIdent || fld.Key.IsMeta() {
			continue
		}
		ls.exprs[fld.Key.Name] = fld.Value
	}
	return ls
}

// localsBlock returns the `locals:` object from a parsed file, or nil
// when the file is absent or declares no locals.
func localsBlock(f *lang.File) *lang.ObjectLit {
	if f == nil || f.Body == nil {
		return nil
	}
	for _, fld := range f.Body.Fields {
		if fld.Key.Kind != lang.FieldIdent || fld.Key.Name != "locals" {
			continue
		}
		if obj, ok := fld.Value.(*lang.ObjectLit); ok {
			return obj
		}
		return nil
	}
	return nil
}

// force evaluates the named local against ctx and returns its value. A
// local that reads an upstream that has not run yet propagates
// ErrEvalNotFound unchanged so the caller can defer it. A local that
// refers back to itself through the chain is reported as a cycle.
func (ls *localScope) force(name string, ctx *EvalContext) (any, error) {
	if v, ok := ls.values[name]; ok {
		return v, nil
	}
	expr, ok := ls.exprs[name]
	if !ok {
		return nil, fmt.Errorf("eval: local %q is not declared: %w", name, ErrEvalNotFound)
	}
	if ls.forcing[name] {
		return nil, fmt.Errorf("eval: local %q refers to itself through a cycle", name)
	}
	ls.forcing[name] = true
	defer delete(ls.forcing, name)
	v, err := Eval(expr, ctx)
	if err != nil {
		return nil, err
	}
	ls.values[name] = v
	return v, nil
}

// evalLocal resolves a `local.<name>[.field...]` reference. The first
// segment names the local; remaining segments navigate into its value.
func evalLocal(p *lang.DotPath, ctx *EvalContext) (any, error) {
	if len(p.Segments) == 0 || p.Segments[0].Name == "" {
		return nil, fmt.Errorf("eval: local reference needs a name")
	}
	name := p.Segments[0].Name
	if ctx.locals == nil {
		return nil, fmt.Errorf("eval: local %q is not declared: %w", name, ErrEvalNotFound)
	}
	base, err := ctx.locals.force(name, ctx)
	if err != nil {
		return nil, err
	}
	return navigateSegments(base, "local."+name, p.Segments[1:], ctx)
}
