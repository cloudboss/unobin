package goschema

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/typecheck"
	"github.com/stretchr/testify/require"
)

func TestReadExtractsSetConstraints(t *testing.T) {
	schema, warnings, err := Read("testdata/constraints")
	require.NoError(t, err)
	require.Empty(t, warnings)
	require.Contains(t, schema.Resources, "cert")

	want := []lang.ConstraintSpec{
		{Kind: "exactly-one-of", Fields: []string{"self-signed", "acm-arn", "pem-bundle"}},
		{Kind: "at-least-one-of", Fields: []string{"self-signed", "acm-arn"}},
		{Kind: "at-most-one-of", Fields: []string{"acm-arn", "pem-bundle"}},
		{Kind: "required-together", Fields: []string{"pem-bundle", "private-key"}},
		{Kind: "required-with", Fields: []string{"pem-bundle", "private-key"}},
		{Kind: "forbidden-with", Fields: []string{"acm-arn", "renew-before"}},
	}
	require.Equal(t, want, schema.Resources["cert"].Constraints)
}

func TestExtractedConstraintsCheckAgainstValues(t *testing.T) {
	schema, _, err := Read("testdata/constraints")
	require.NoError(t, err)
	entries, perr := lang.ParseSpecs(schema.Resources["cert"].Constraints)
	require.Equal(t, 0, perr.Len(), "specs should parse: %v", perr.Err())

	// One source set, nothing forbidden: every constraint holds.
	ok := lang.CheckConstraintEntries(entries, map[string]any{"self-signed": true}, nil)
	require.Equal(t, 0, ok.Len(), "a valid input set should pass: %v", ok.Err())

	// Two sources set, and a pem bundle without its key: several fail.
	bad := lang.CheckConstraintEntries(entries, map[string]any{
		"acm-arn":    "arn",
		"pem-bundle": "pem",
	}, nil)
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
		ctx := &runtime.EvalContext{Vars: values}
		eval := func(e lang.Expr) (any, error) { return runtime.Eval(e, ctx) }
		return lang.CheckConstraintEntries(entries, values, eval).Len()
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

	require.Equal(t, map[string]bool{"shout": true, "reverse": true}, schema.Functions)
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
			"thing": runtime.MakeResource[Thing, *ThingOutput](),
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
	require.Empty(t, warnings)

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

func TestReadMarksPointerStructFieldsOptional(t *testing.T) {
	schema, warnings, err := Read("testdata/nested")
	require.NoError(t, err)
	require.Empty(t, warnings)

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
	require.Empty(t, warnings)

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
