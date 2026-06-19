package goschema

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/typecheck"
	"github.com/stretchr/testify/require"
)

// The nested and crosspkg fixtures' DBOutput refers to itself through
// its Self field, which no language type expresses; every Read of
// either fixture reports that one warning.
const nestedSelfWarning = `resource "db" output: field self: Go type *DBOutput does not ` +
	`fully map to language types, so reads of it are unchecked`

func TestReadExtractsConfigurationSchema(t *testing.T) {
	schema, warnings, err := Read("testdata/configuration")
	require.NoError(t, err)
	require.Empty(t, warnings)
	assumeRole := typecheck.TObject([]typecheck.ObjectField{
		{Name: "role-arn", Type: typecheck.TString()},
		{Name: "external", Type: typecheck.TString(), Optional: true},
	})
	endpoint := typecheck.TObject([]typecheck.ObjectField{
		{Name: "host", Type: typecheck.TString()},
	})
	want := map[string]typecheck.Type{
		"region":      typecheck.TString(),
		"profile":     typecheck.TOptional(typecheck.TString()),
		"retries":     typecheck.TInteger(),
		"ratio":       typecheck.TOptional(typecheck.TNumber()),
		"verbose":     typecheck.TBoolean(),
		"tags":        typecheck.TMap(typecheck.TString()),
		"subnets":     typecheck.TList(typecheck.TString()),
		"extra":       typecheck.TOpaque(),
		"endpoint":    endpoint,
		"assume-role": typecheck.TOptional(assumeRole),
	}
	require.Equal(t, want, schema.Configuration)
	require.True(t, schema.HasConfiguration)
}

func TestReadWarnsWhenConfigurationNewIsNotALiteral(t *testing.T) {
	schema, warnings, err := Read("testdata/badnew")
	require.NoError(t, err)
	require.Nil(t, schema.Configuration)
	require.True(t, schema.HasConfiguration)
	require.Len(t, warnings, 1)
	require.Contains(t, warnings[0], "library configuration")
}

func TestReadLeavesConfigurationNilWhenAbsent(t *testing.T) {
	schema, _, err := Read("testdata/samepkg")
	require.NoError(t, err)
	require.Nil(t, schema.Configuration)
	require.False(t, schema.HasConfiguration)
}

func TestReadExtractsSetConstraints(t *testing.T) {
	schema, warnings, err := Read("testdata/constraints")
	require.NoError(t, err)
	require.Empty(t, warnings)
	require.Contains(t, schema.Resources, "cert")

	want := []lang.ConstraintSpec{
		{Kind: "exactly-one-of", Fields: []string{"var.self-signed", "var.acm-arn", "var.pem-bundle"}},
		{Kind: "at-least-one-of", Fields: []string{"var.self-signed", "var.acm-arn"}},
		{Kind: "at-most-one-of", Fields: []string{"var.acm-arn", "var.pem-bundle"}},
		{Kind: "required-together", Fields: []string{"var.pem-bundle", "var.private-key"}},
		{Kind: "required-with", Fields: []string{"var.pem-bundle", "var.private-key"}},
		{Kind: "forbidden-with", Fields: []string{"var.acm-arn", "var.renew-before"}},
	}
	require.Equal(t, want, schema.Resources["cert"].Constraints)
}

func TestExtractedConstraintsCheckAgainstValues(t *testing.T) {
	schema, _, err := Read("testdata/constraints")
	require.NoError(t, err)
	entries, perr := lang.ParseSpecs(schema.Resources["cert"].Constraints)
	require.Equal(t, 0, perr.Len(), "specs should parse: %v", perr.Err())

	// One source set, nothing forbidden: every constraint holds.
	ok := lang.CheckConstraintEntries(entries,
		map[string]any{"self-signed": true}, nil, lang.DisplayNodeRelative)
	require.Equal(t, 0, ok.Len(), "a valid input set should pass: %v", ok.Err())

	// Two sources set, and a pem bundle without its key: several fail.
	bad := lang.CheckConstraintEntries(entries, map[string]any{
		"acm-arn":    "arn",
		"pem-bundle": "pem",
	}, nil, lang.DisplayNodeRelative)
	require.Greater(t, bad.Len(), 0, "a conflicting input set should fail")
}

func TestReadExtractsPredicateConstraints(t *testing.T) {
	schema, warnings, err := Read("testdata/constraints")
	require.NoError(t, err)
	require.Empty(t, warnings)
	require.Contains(t, schema.Resources, "policy")

	specs := schema.Resources["policy"].Constraints
	require.Equal(t, []lang.ConstraintSpec{
		{
			Kind:    "predicate",
			When:    "(var.tier == 'prod')",
			Require: "(var.backups == true)",
			Message: "prod requires backups",
		},
		{
			Kind: "predicate",
			When: "true",
			Require: "(var.max-size == null || var.min-size == null" +
				" || var.max-size >= var.min-size)",
		},
		{
			Kind:    "predicate",
			When:    "true",
			Require: "(var.region == 'us-east-1' || var.region == 'us-west-2')",
		},
	}, specs)

	entries, perr := lang.ParseSpecs(specs)
	require.Equal(t, 0, perr.Len(), "specs should parse: %v", perr.Err())

	// Evaluate the rendered when/require through the real evaluator, the
	// same path a UB predicate takes at plan.
	check := func(values map[string]any) int {
		eval := func(e lang.Expr, binds []lang.EachBinding) (any, error) {
			ctx := &runtime.EvalContext{Vars: values}
			for _, b := range binds {
				if ctx.Each == nil {
					ctx.Each = map[string]lang.EachValue{}
				}
				ctx.Each[b.Name] = lang.EachValue{Key: b.Key, Value: b.Value}
			}
			return runtime.Eval(e, ctx)
		}
		return lang.CheckConstraintEntries(entries, values, eval, lang.DisplayNodeRelative).Len()
	}
	base := func() map[string]any {
		return map[string]any{
			"tier": "dev", "backups": nil,
			"min-size": nil, "max-size": nil, "region": "us-east-1",
		}
	}

	require.Equal(t, 0, check(base()), "dev with a valid region passes")

	prodNoBackups := base()
	prodNoBackups["tier"] = "prod"
	require.Equal(t, 1, check(prodNoBackups), "prod without backups fails")

	prodBackups := base()
	prodBackups["tier"] = "prod"
	prodBackups["backups"] = true
	require.Equal(t, 0, check(prodBackups), "prod with backups passes")

	badSizes := base()
	badSizes["min-size"] = int64(5)
	badSizes["max-size"] = int64(2)
	require.Equal(t, 1, check(badSizes), "max below min fails")

	badRegion := base()
	badRegion["region"] = "eu-west-1"
	require.Equal(t, 1, check(badRegion), "a region outside the set fails")
}

func TestConstraintRootMustBeTheReceiver(t *testing.T) {
	src := `package x

import "github.com/cloudboss/unobin/pkg/constraint"

type T struct {
	A *string
	B *string
}

var q T

func (v T) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.RequiredTogether(v.A, q.B),
	}
}
`
	f, err := parser.ParseFile(token.NewFileSet(), "x.go", src, 0)
	require.NoError(t, err)
	errs := &[]error{}
	w := newWalker([]ModuleRoot{{}}, []*ast.File{f}, map[string][]*ast.File{}, errs, nil)
	specs := w.constraintsFromType("T")
	require.Empty(t, specs)
	require.NotEmpty(t, *errs)
	require.Contains(t, (*errs)[0].Error(), `"q"`)
}

func TestForEachRejectsUnsupportedForms(t *testing.T) {
	const prologue = `package x

import "github.com/cloudboss/unobin/pkg/constraint"

type T struct {
	Items []Item
	Name  *string
}

type Item struct {
	A *string
	B *string
}
`
	tests := []struct {
		name    string
		method  string
		wantErr string
	}{
		{"message on ForEach",
			`func (v T) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.ForEach(v.Items, func(it Item) []constraint.Constraint {
			return []constraint.Constraint{
				constraint.RequiredTogether(it.A, it.B),
			}
		}).Message("m"),
	}
}`,
			"Message applies to the constraints inside ForEach"},
		{"list argument not a field",
			`func (v T) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.ForEach([]Item{}, func(it Item) []constraint.Constraint {
			return []constraint.Constraint{
				constraint.RequiredTogether(it.A, it.B),
			}
		}),
	}
}`,
			"ForEach list must be a struct field selector"},
		{"list field not a list",
			`func (v T) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.ForEach(v.Name, func(it Item) []constraint.Constraint {
			return []constraint.Constraint{
				constraint.RequiredTogether(it.A, it.B),
			}
		}),
	}
}`,
			"must be a slice of in-library structs"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := prologue + "\n" + tt.method + "\n"
			f, err := parser.ParseFile(token.NewFileSet(), "x.go", src, 0)
			require.NoError(t, err)
			errs := &[]error{}
			w := newWalker([]ModuleRoot{{}}, []*ast.File{f}, map[string][]*ast.File{}, errs, nil)
			specs := w.constraintsFromType("T")
			require.Empty(t, specs)
			require.NotEmpty(t, *errs)
			require.Contains(t, (*errs)[0].Error(), tt.wantErr)
		})
	}
}

func TestReadExtractsNestedConstraints(t *testing.T) {
	schema, warnings, err := Read("testdata/nested")
	require.NoError(t, err)
	require.Equal(t, []string{nestedSelfWarning}, warnings)
	require.Contains(t, schema.Resources, "db")

	want := []lang.ConstraintSpec{
		{Kind: "exactly-one-of", Fields: []string{"var.code.inline", "var.code.from-file"}},
		{
			Kind:    "predicate",
			When:    "(var.code.signing != null)",
			Require: "(var.code.signing.key-arn != null)",
			Message: "signing requires a key arn",
		},
		{Kind: "required-together", Fields: []string{"var.listeners[0].cert", "var.listeners[0].key"}},
		{Kind: "exactly-one-of", Fields: []string{"var.replicas[*].inline", "var.replicas[*].from-file"}},
		{Kind: "required-with", Fields: []string{"var.replicas[*].tls", "var.ca-cert"}},
		{
			Kind:    "predicate",
			When:    "(@each.value.tls == true)",
			Require: "(@each.value.cert != null)",
			Message: "tls requires a cert",
			ForEach: "var.replicas",
		},
	}
	require.Equal(t, want, schema.Resources["db"].Constraints)
}

func TestExtractedNestedConstraintsCheckAgainstValues(t *testing.T) {
	schema, _, err := Read("testdata/nested")
	require.NoError(t, err)
	entries, perr := lang.ParseSpecs(schema.Resources["db"].Constraints)
	require.Equal(t, 0, perr.Len(), "specs should parse: %v", perr.Err())

	eval := func(values map[string]any) lang.ConstraintEvalFunc {
		return func(e lang.Expr, binds []lang.EachBinding) (any, error) {
			ctx := &runtime.EvalContext{Vars: values, MissingAsNull: true}
			for _, b := range binds {
				if ctx.Each == nil {
					ctx.Each = map[string]lang.EachValue{}
				}
				ctx.Each[b.Name] = lang.EachValue{Key: b.Key, Value: b.Value}
			}
			return runtime.Eval(e, ctx)
		}
	}

	// One code source set, signing present with a key arn: all hold.
	good := map[string]any{
		"code": map[string]any{
			"inline":  "echo hi",
			"signing": map[string]any{"key-arn": "arn"},
		},
	}
	ok := lang.CheckConstraintEntries(entries, good, eval(good), lang.DisplayNodeRelative)
	require.Equal(t, 0, ok.Len(), "valid nested input should pass: %v", ok.Err())

	// Both sources set, and signing present without a key arn: both fail.
	bad := map[string]any{
		"code": map[string]any{
			"inline":    "echo hi",
			"from-file": "build.sh",
			"signing":   map[string]any{},
		},
	}
	got := lang.CheckConstraintEntries(entries, bad, eval(bad), lang.DisplayNodeRelative)
	require.Equal(t, 2, got.Len(), "two violations expected: %v", got.Err())

	// ForEach(replicas): the second replica sets both code sources, and the
	// third turns on tls without the ca-cert the rule requires.
	badReplicas := map[string]any{
		"code": map[string]any{"inline": "echo hi"},
		"replicas": []any{
			map[string]any{"inline": "a"},
			map[string]any{"inline": "a", "from-file": "f"},
			map[string]any{"inline": "a", "tls": true},
		},
	}
	got = lang.CheckConstraintEntries(entries, badReplicas,
		eval(badReplicas), lang.DisplayNodeRelative)
	require.Equal(t, 3, got.Len(), "three replica violations expected: %v", got.Err())
	require.Contains(t, got.Err().Error(), "got 2 (replicas[1].inline, replicas[1].from-file)")
	require.Contains(t, got.Err().Error(), `"replicas[2].tls" is set`)
	require.Contains(t, got.Err().Error(), "tls requires a cert (replicas[2])")
}

func TestFlattenSelector(t *testing.T) {
	tests := []struct {
		src      string
		wantRoot string
		want     []selectorHop
		ok       bool
	}{
		{"v.Field", "v", []selectorHop{{name: "Field"}}, true},
		{"v.Code.Inline", "v", []selectorHop{{name: "Code"}, {name: "Inline"}}, true},
		{"v.Code.Signing.KeyArn", "v",
			[]selectorHop{{name: "Code"}, {name: "Signing"}, {name: "KeyArn"}}, true},
		{"v.Listeners[0].Cert", "v",
			[]selectorHop{{name: "Listeners", indexes: []int{0}}, {name: "Cert"}}, true},
		{"v.Matrix[0][2].X", "v",
			[]selectorHop{{name: "Matrix", indexes: []int{0, 2}}, {name: "X"}}, true},
		{"foo().Bar", "", nil, false},
		{"a[0].B", "", nil, false},
		{"v.Listeners[i].Cert", "", nil, false},
		{"v.Listeners[-1].Cert", "", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			expr, err := parser.ParseExpr(tt.src)
			require.NoError(t, err)
			sel, ok := expr.(*ast.SelectorExpr)
			require.True(t, ok)
			root, hops, ok := flattenSelector(sel)
			require.Equal(t, tt.ok, ok)
			if tt.ok {
				require.Equal(t, tt.wantRoot, root)
				require.Equal(t, tt.want, hops)
			}
		})
	}
}

func TestFieldPath(t *testing.T) {
	files, err := parsePackageDir("testdata/nested")
	require.NoError(t, err)
	errs := &[]error{}
	w := newWalker([]ModuleRoot{{Dir: "testdata/nested"}}, files, map[string][]*ast.File{}, errs, nil)

	tests := []struct {
		name string
		hops []selectorHop
		want string
		ok   bool
	}{
		{"single hop", []selectorHop{{name: "Name"}}, "name", true},
		{"two hop", []selectorHop{{name: "Code"}, {name: "Inline"}}, "code.inline", true},
		{"two hop other", []selectorHop{{name: "Code"}, {name: "FromFile"}},
			"code.from-file", true},
		{"three hop through pointer",
			[]selectorHop{{name: "Code"}, {name: "Signing"}, {name: "KeyArn"}},
			"code.signing.key-arn", true},
		{"indexed hop into element field",
			[]selectorHop{{name: "Listeners", indexes: []int{0}}, {name: "Cert"}},
			"listeners[0].cert", true},
		{"index on a non-list field",
			[]selectorHop{{name: "Code", indexes: []int{0}}, {name: "Inline"}}, "", false},
		{"unknown top field", []selectorHop{{name: "Bogus"}}, "", false},
		{"unknown nested leaf", []selectorHop{{name: "Code"}, {name: "Bogus"}}, "", false},
		{"descend into non-struct",
			[]selectorHop{{name: "Code"}, {name: "Inline"}, {name: "X"}}, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := w.fieldPath("DB", tt.hops)
			require.Equal(t, tt.ok, ok)
			if tt.ok {
				require.Equal(t, tt.want, got)
			}
		})
	}
	require.Empty(t, *errs, "fieldPath records no errors itself")
}

func TestReadSamePackage(t *testing.T) {
	schema, warnings, err := Read("testdata/samepkg")
	require.NoError(t, err)
	require.Empty(t, warnings)
	require.NotNil(t, schema)

	require.Contains(t, schema.Actions, "do")
	do := schema.Actions["do"]
	require.Equal(t, map[string]typecheck.Type{
		"what": typecheck.TString(),
	}, do.Inputs)
	require.Equal(t, map[string]typecheck.Type{
		"result":   typecheck.TString(),
		"duration": typecheck.TInteger(),
		"tags":     typecheck.TList(typecheck.TString()),
	}, do.Outputs)

	require.Contains(t, schema.Actions, "do2")
	require.Equal(t, do.Outputs, schema.Actions["do2"].Outputs,
		"the alias should resolve to the same field set")

	fns := schema.Functions
	require.Len(t, fns, 3)

	require.Len(t, fns["upper"].Params, 1)
	require.True(t, fns["upper"].Params[0].Equal(typecheck.TString()))
	require.True(t, fns["upper"].Result.Equal(typecheck.TString()))
	require.Nil(t, fns["upper"].Variadic)

	require.Len(t, fns["pair"].Params, 2)
	require.True(t, fns["pair"].Params[0].Equal(typecheck.TString()))
	require.True(t, fns["pair"].Params[1].Equal(typecheck.TString()))
	require.Nil(t, fns["pair"].Variadic)

	require.Len(t, fns["join"].Params, 1)
	require.True(t, fns["join"].Params[0].Equal(typecheck.TString()))
	require.NotNil(t, fns["join"].Variadic)
	require.True(t, fns["join"].Variadic.Equal(typecheck.TString()))
	require.True(t, fns["join"].Result.Equal(typecheck.TString()))
}

func TestReadSubpackage(t *testing.T) {
	schema, warnings, err := Read("testdata/subpkg")
	require.NoError(t, err)
	require.Empty(t, warnings)

	require.Contains(t, schema.Resources, "thing")
	thing := schema.Resources["thing"]
	require.Equal(t, map[string]typecheck.Type{
		"name": typecheck.TString(),
	}, thing.Inputs)
	require.Equal(t, map[string]typecheck.Type{
		"id":         typecheck.TString(),
		"cidr-block": typecheck.TString(),
		"replicas":   typecheck.TOptional(typecheck.TInteger()),
	}, thing.Outputs)

	require.Contains(t, schema.DataSources, "ami")
	ami := schema.DataSources["ami"]
	require.Equal(t, map[string]typecheck.Type{
		"image-id": typecheck.TString(),
	}, ami.Inputs)
	require.Equal(t, map[string]typecheck.Type{
		"architecture": typecheck.TString(),
	}, ami.Outputs)
}

func TestReadDerivesKebabFromFieldNameWhenTagAbsent(t *testing.T) {
	schema, warnings, err := Read("testdata/untagged")
	require.NoError(t, err)
	require.Empty(t, warnings)

	require.Contains(t, schema.Resources, "thing")
	thing := schema.Resources["thing"]
	require.Equal(t, map[string]typecheck.Type{
		"id":           typecheck.TString(),
		"cidr-block":   typecheck.TString(),
		"https-proxy":  typecheck.TString(),
		"explicit-tag": typecheck.TString(),
	}, thing.Outputs)
}

func TestReadWarnsWhenOutputTypeMissing(t *testing.T) {
	dir := t.TempDir()
	src := []byte(`package lib

import "github.com/cloudboss/unobin/pkg/runtime"

func Library() *runtime.Library {
	return &runtime.Library{
		Name: "lib",
		Resources: map[string]runtime.ResourceRegistration{
			"thing": runtime.MakeResource[Thing, *ThingOutput, any](),
		},
	}
}

type Thing struct{}
`)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "library.go"), src, 0o644))

	schema, warnings, err := Read(dir)
	require.NoError(t, err)
	require.NotNil(t, schema)
	require.Contains(t, schema.Resources, "thing")
	require.Nil(t, schema.Resources["thing"].Outputs)

	require.Len(t, warnings, 1)
	require.Contains(t, warnings[0], `"thing"`)
	require.Contains(t, warnings[0], "ThingOutput")
}

func TestReadExpandsNestedStructTypes(t *testing.T) {
	schema, warnings, err := Read("testdata/nested")
	require.NoError(t, err)
	require.Equal(t, []string{nestedSelfWarning}, warnings)

	require.Contains(t, schema.Resources, "db")
	db := schema.Resources["db"]

	endpoint := typecheck.TObject([]typecheck.ObjectField{
		{Name: "host", Type: typecheck.TString()},
		{Name: "port", Type: typecheck.TInteger()},
	})

	require.Len(t, db.Outputs, 4)
	require.True(t, db.Outputs["id"].Equal(typecheck.TString()),
		"got %s", db.Outputs["id"])
	require.True(t, db.Outputs["endpoint"].Equal(endpoint),
		"got %s", db.Outputs["endpoint"])
	require.True(t,
		db.Outputs["replicas"].Equal(typecheck.TList(endpoint)),
		"got %s", db.Outputs["replicas"])
	// Self is *DBOutput while DBOutput is being walked, so the
	// cycle guard returns Unknown for the recursive reference.
	require.True(t,
		db.Outputs["self"].Equal(typecheck.TOptional(typecheck.TUnknown())),
		"recursive self should hit the cycle guard, got %s",
		db.Outputs["self"])
}

// TestReadEmitsNestedFieldsInDeclarationOrder pins the field order of
// an expanded nested object to the struct's declaration order, looping
// because a map-driven order can be right by luck once. Equality is on
// the Fields slice itself; Type.Equal compares order-insensitively.
func TestReadEmitsNestedFieldsInDeclarationOrder(t *testing.T) {
	src := `package lib

import "github.com/cloudboss/unobin/pkg/runtime"

func Library() *runtime.Library {
	return &runtime.Library{
		Name: "lib",
		Resources: map[string]runtime.ResourceRegistration{
			"thing": runtime.MakeResource[Thing, *ThingOutput, any](),
		},
	}
}

type Thing struct {
	Settings Settings
}

type Settings struct {
	Zeta         string
	Alpha        string
	Mid          *int64
	Beta         bool
	Omega        int64
	Gamma        string
	Pair1, Pair2 string
}

type ThingOutput struct {
	ID string
}
`
	want := []typecheck.ObjectField{
		{Name: "zeta", Type: typecheck.TString()},
		{Name: "alpha", Type: typecheck.TString()},
		{Name: "mid", Type: typecheck.TInteger(), Optional: true},
		{Name: "beta", Type: typecheck.TBoolean()},
		{Name: "omega", Type: typecheck.TInteger()},
		{Name: "gamma", Type: typecheck.TString()},
		{Name: "pair1", Type: typecheck.TString()},
		{Name: "pair2", Type: typecheck.TString()},
	}
	for range 10 {
		schema, warnings, err := readConstraintLibrary(t, src)
		require.NoError(t, err)
		require.Empty(t, warnings)
		settings := schema.Resources["thing"].Inputs["settings"]
		require.Equal(t, typecheck.Object, settings.Kind)
		require.Equal(t, want, settings.Fields)
	}
}

func TestReadMarksPointerStructFieldsOptional(t *testing.T) {
	schema, warnings, err := Read("testdata/nested")
	require.NoError(t, err)
	require.Equal(t, []string{nestedSelfWarning}, warnings)

	require.Contains(t, schema.Resources, "db")
	code := schema.Resources["db"].Inputs["code"]

	// signing is itself an optional nested object, and its own
	// fields carry their own optionality, so the marking has to
	// hold at every level of nesting, not just the first.
	signing := typecheck.TObject([]typecheck.ObjectField{
		{Name: "key-arn", Type: typecheck.TString(), Optional: true},
		{Name: "profile", Type: typecheck.TString()},
	})
	want := typecheck.TObject([]typecheck.ObjectField{
		{Name: "inline", Type: typecheck.TString(), Optional: true},
		{Name: "from-file", Type: typecheck.TString(), Optional: true},
		{Name: "signing", Type: signing, Optional: true},
	})
	require.True(t, code.Equal(want), "got %s", code)
}

func TestReadExpandsCrossPackageStructTypes(t *testing.T) {
	schema, warnings, err := Read("testdata/crosspkg")
	require.NoError(t, err)
	require.Equal(t, []string{nestedSelfWarning}, warnings)

	require.Contains(t, schema.Resources, "db")
	db := schema.Resources["db"]

	port := typecheck.TObject([]typecheck.ObjectField{
		{Name: "number", Type: typecheck.TInteger()},
		{Name: "protocol", Type: typecheck.TString()},
	})
	endpoint := typecheck.TObject([]typecheck.ObjectField{
		{Name: "host", Type: typecheck.TString()},
		{Name: "port", Type: port},
	})

	require.Len(t, db.Outputs, 4)
	require.True(t, db.Outputs["id"].Equal(typecheck.TString()),
		"got %s", db.Outputs["id"])
	require.True(t, db.Outputs["endpoint"].Equal(endpoint),
		"got %s", db.Outputs["endpoint"])
	require.True(t,
		db.Outputs["replicas"].Equal(typecheck.TList(endpoint)),
		"got %s", db.Outputs["replicas"])
	require.True(t,
		db.Outputs["self"].Equal(typecheck.TOptional(typecheck.TUnknown())),
		"recursive self should hit the cycle guard, got %s",
		db.Outputs["self"])
}

func TestReadCarriesSensitiveTag(t *testing.T) {
	schema, warnings, err := Read("testdata/sensitive")
	require.NoError(t, err)
	require.Empty(t, warnings)

	require.Contains(t, schema.Resources, "secret")
	secret := schema.Resources["secret"]
	require.Equal(t, []string{"password"}, secret.SensitiveInputs)
	require.Equal(t, []string{"value"}, secret.SensitiveOutputs)
}

func TestReadErrorsWhenNoModuleFunc(t *testing.T) {
	dir := t.TempDir()
	src := []byte("package empty\n\nfunc Other() int { return 0 }\n")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "empty.go"), src, 0o644))

	_, _, err := Read(dir)
	require.Error(t, err)
}

func TestReadRejectsUnknownUBOption(t *testing.T) {
	_, _, err := Read("testdata/badoption")
	require.Error(t, err)
	require.Contains(t, err.Error(), "sensitiv")
}

func TestReadWarnsOnUnmappableFieldTypes(t *testing.T) {
	schema, warnings, err := Read("testdata/unmappable")
	require.NoError(t, err)
	require.Equal(t, []string{
		`resource "thing" input: field updates: Go type chan string does not fully map ` +
			`to language types, so reads of it are unchecked`,
		`resource "thing" output: field seen: Go type time.Time does not fully map ` +
			`to language types, so reads of it are unchecked`,
	}, warnings)

	thing := schema.Resources["thing"]
	require.NotNil(t, thing)
	require.True(t, thing.Inputs["updates"].Equal(typecheck.TUnknown()))
	require.True(t, thing.Outputs["seen"].Equal(typecheck.TUnknown()))
	require.True(t, thing.Inputs["name"].Equal(typecheck.TString()),
		"mappable fields extract as usual")
}

func TestMakeFuncRegistrationErrors(t *testing.T) {
	const prologue = `package x

import "github.com/cloudboss/unobin/pkg/runtime"

func Library() *runtime.Library {
	return &runtime.Library{
		Functions: map[string]runtime.FunctionType{
`
	tests := []struct {
		name    string
		entry   string
		extra   string
		wantErr string
	}{
		{"unknown function name",
			`"x": runtime.MakeFunc("x", "d", nosuch),`, "",
			`function "x": MakeFunc references nosuch, which is not a function in the package`},
		{"one result",
			`"x": runtime.MakeFunc("x", "d", oneResult),`,
			"func oneResult(s string) string { return s }",
			`function "x" must return (value, error), got 1 result`},
		{"second result not error",
			`"x": runtime.MakeFunc("x", "d", twoStrings),`,
			`func twoStrings(s string) (string, string) { return s, s }`,
			`function "x" must return (value, error)`},
		{"unsupported parameter",
			`"x": runtime.MakeFunc("x", "d", badParam),`,
			"func badParam(ch chan int) (bool, error) { return true, nil }",
			`function "x" parameter 1 has an unsupported type`},
		{"int parameter is not the currency",
			`"x": runtime.MakeFunc("x", "d", intParam),`,
			"func intParam(n int) (bool, error) { return true, nil }",
			`function "x" parameter 1 has an unsupported type`},
		{"unsupported result",
			`"x": runtime.MakeFunc("x", "d", badResult),`,
			"func badResult(s string) (chan int, error) { return nil, nil }",
			`function "x" result has an unsupported type`},
		{"function type literal",
			`"x": runtime.FunctionType{Name: "x", ArgCount: 1},`, "",
			`function "x": register with runtime.MakeFunc; ` +
				`a FunctionType literal declares no types`},
		{"some other call",
			`"x": helper(),`,
			"func helper() runtime.FunctionType { return runtime.FunctionType{} }",
			`function "x": register with runtime.MakeFunc`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := prologue + "\t\t\t" + tt.entry + "\n\t\t},\n\t}\n}\n\n" + tt.extra + "\n"
			f, err := parser.ParseFile(token.NewFileSet(), "x.go", src, 0)
			require.NoError(t, err)
			fn := findModuleFunc([]*ast.File{f})
			require.NotNil(t, fn)
			errs := &[]error{}
			extractFunctions(fn, []*ast.File{f}, errs)
			require.NotEmpty(t, *errs)
			require.Contains(t, (*errs)[0].Error(), tt.wantErr)
		})
	}
}

func TestMakeFuncRegistrationFromLiteral(t *testing.T) {
	src := `package x

import "github.com/cloudboss/unobin/pkg/runtime"

func Library() *runtime.Library {
	return &runtime.Library{
		Functions: map[string]runtime.FunctionType{
			"flag": runtime.MakeFunc("flag", "d", func(names []string, on bool) (bool, error) {
				return on, nil
			}),
		},
	}
}
`
	f, err := parser.ParseFile(token.NewFileSet(), "x.go", src, 0)
	require.NoError(t, err)
	fn := findModuleFunc([]*ast.File{f})
	require.NotNil(t, fn)
	errs := &[]error{}
	out := extractFunctions(fn, []*ast.File{f}, errs)
	require.Empty(t, *errs)
	flag := out["flag"]
	require.Len(t, flag.Params, 2)
	require.True(t, flag.Params[0].Equal(typecheck.TList(typecheck.TString())))
	require.True(t, flag.Params[1].Equal(typecheck.TBoolean()))
	require.Nil(t, flag.Variadic)
	require.True(t, flag.Result.Equal(typecheck.TBoolean()))
}

func TestReadConfigurationFromExtraRoot(t *testing.T) {
	schema, warnings, err := Read("testdata/extroot/library", ModuleRoot{
		Path: "example.com/shared",
		Dir:  filepath.Join("testdata", "extroot", "shared"),
	})
	require.NoError(t, err)
	require.Empty(t, warnings)
	assumeRole := typecheck.TObject([]typecheck.ObjectField{
		{Name: "role-arn", Type: typecheck.TString()},
		{Name: "external-id", Type: typecheck.TString(), Optional: true},
	})
	tuning := typecheck.TObject([]typecheck.ObjectField{
		{Name: "max-attempts", Type: typecheck.TInteger()},
	})
	want := map[string]typecheck.Type{
		"region":      typecheck.TString(),
		"endpoint":    typecheck.TOptional(typecheck.TString()),
		"assume-role": typecheck.TOptional(assumeRole),
		"tuning":      tuning,
	}
	require.Equal(t, want, schema.Configuration)
	require.True(t, schema.HasConfiguration)
}

func TestReadWarnsWhenExtraRootAbsent(t *testing.T) {
	schema, warnings, err := Read("testdata/extroot/library")
	require.NoError(t, err)
	require.Nil(t, schema.Configuration)
	require.True(t, schema.HasConfiguration)
	require.Len(t, warnings, 1)
	require.Contains(t, warnings[0], "library configuration")
}
