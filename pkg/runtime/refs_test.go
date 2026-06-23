package runtime

import (
	"fmt"
	"strings"
	"testing"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/lang"
)

func TestRefsFixtures(t *testing.T) {
	ubtest.Run(t, "testdata/ub/refs", refsFixtureDriver)
}

func refsFixtureDriver(name string, src []byte) (string, []string) {
	f, err := lang.ParseSource(name+".ub", src)
	if err != nil {
		return "", []string{err.Error()}
	}
	locals, diags := refsFixtureLocals(f)
	if len(diags) > 0 {
		return "", diags
	}
	sections := []struct {
		name string
		refs func(lang.Expr) []string
	}{
		{name: "refs", refs: Refs},
		{name: "refs-with-locals", refs: func(e lang.Expr) []string {
			return refsWithLocals(e, locals)
		}},
		{name: "deferred-refs", refs: func(e lang.Expr) []string {
			return deferredRefs(e, locals)
		}},
	}

	var b strings.Builder
	first := true
	for _, section := range sections {
		obj, diag := refsFixtureObject(f, section.name)
		if diag != "" {
			return "", []string{diag}
		}
		if obj == nil {
			continue
		}
		if !first {
			b.WriteByte('\n')
		}
		first = false
		fmt.Fprintf(&b, "[%s]\n", section.name)
		for _, fld := range obj.Fields {
			field := refsFixtureFieldName(fld)
			if field == "" || fld.Value == nil {
				continue
			}
			fmt.Fprintf(&b, "%s: %s\n", field, refsText(section.refs(fld.Value)))
		}
	}
	return b.String(), nil
}

func refsFixtureLocals(f *lang.File) (map[string]lang.Expr, []string) {
	locals := map[string]lang.Expr{}
	obj, diag := refsFixtureObject(f, "locals")
	if diag != "" {
		return nil, []string{diag}
	}
	if obj == nil {
		return locals, nil
	}
	for _, fld := range obj.Fields {
		name := refsFixtureFieldName(fld)
		if name == "" || fld.Value == nil {
			continue
		}
		locals[name] = fld.Value
	}
	return locals, nil
}

func refsFixtureObject(f *lang.File, name string) (*lang.ObjectLit, string) {
	if f == nil || f.Body == nil {
		return nil, "fixture has no body"
	}
	for _, fld := range f.Body.Fields {
		if refsFixtureFieldName(fld) != name {
			continue
		}
		obj, ok := fld.Value.(*lang.ObjectLit)
		if !ok {
			return nil, fmt.Sprintf("%s must be an object", name)
		}
		return obj, ""
	}
	return nil, ""
}

func refsFixtureFieldName(fld *lang.Field) string {
	if fld == nil {
		return ""
	}
	switch fld.Key.Kind {
	case lang.FieldIdent:
		return fld.Key.Name
	case lang.FieldString:
		return fld.Key.String
	default:
		return ""
	}
}

func refsText(refs []string) string {
	if len(refs) == 0 {
		return "<none>"
	}
	return strings.Join(refs, ", ")
}
