package lang

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/ubtest"
)

func TestValidateCallsFixtures(t *testing.T) {
	ubtest.Run(t, "testdata/ub/calls", func(name string, src []byte) (string, []string) {
		f, err := ParseSource("factory.ub", src)
		if err != nil {
			return "", []string{err.Error()}
		}
		if inputs := TopLevelBlock(f, "inputs"); inputs != nil {
			errs := ValidateInputDeclarations(inputs)
			if errs.Len() > 0 {
				return "", errs.Strings()
			}
		}
		return "", ValidateCalls(f).Strings()
	})
}

func TestValidateComprehensionBindingsFixtures(t *testing.T) {
	ubtest.Run(t, "testdata/ub/comprehensions", func(name string, src []byte) (string, []string) {
		f, err := ParseSource("factory.ub", src)
		if err != nil {
			return "", []string{err.Error()}
		}
		return "", ValidateComprehensionBindings(f).Strings()
	})
}

func stackValueDriver(name string, src []byte) (string, []string) {
	f, err := ParseSource("stack-values.ub", src)
	if err != nil {
		return "", []string{err.Error()}
	}
	blocks := fixtureTopLevelBlocks(f)
	errs := NewErrorList(0)
	locals := stackLocalNames(blocks["locals"])
	if obj, ok := blocks["locals"].(*ObjectLit); ok {
		mergeErrors(errs, ValidateLocals(obj))
		mergeErrors(errs, ValidateStackLocals(obj))
	}
	if obj, ok := blocks["factory"].(*ObjectLit); ok {
		mergeErrors(errs, ValidateStackFactory(obj, locals))
	}
	return "", errs.Strings()
}

func fixtureTopLevelBlocks(f *File) map[string]Expr {
	out := make(map[string]Expr, len(f.Body.Fields))
	for _, fld := range f.Body.Fields {
		if fld.Key.Kind == FieldIdent && !fld.Key.IsMeta() {
			out[fld.Key.Name] = fld.Value
		}
	}
	return out
}

func TestValidateStackValueFixtures(t *testing.T) {
	ubtest.Run(t, "testdata/ub/stack-values", stackValueDriver)
}

func parseInputsBlock(t *testing.T, src string) *ObjectLit {
	t.Helper()
	f, err := ParseSource("", []byte(src))
	require.NoError(t, err)
	require.NotEmpty(t, f.Body.Fields)
	require.Equal(t, "inputs", f.Body.Fields[0].Key.Name)
	o, ok := f.Body.Fields[0].Value.(*ObjectLit)
	require.True(t, ok, "expected `inputs:` to be an object literal")
	return o
}

func TestValidateInputBadType(t *testing.T) {
	src := ubtest.ReadFixture(t, "testdata/ub/inputs/invalid/bad-list-constructor.ub")
	errs := ValidateInputDeclarations(parseInputsBlock(t, src))
	require.Equal(t, 1, errs.Len())
	require.Equal(t, ErrType, errs.Errors()[0].Kind)
}

func TestValidateInputDeclarationsStoresParsedTypes(t *testing.T) {
	src := ubtest.ReadValidFixture(t, "testdata/ub/inputs", "parsed-object-default")
	block := parseInputsBlock(t, src)

	errs := ValidateInputDeclarations(block)
	require.Equal(t, 0, errs.Len(), errs.Error())

	decl := block.Fields[0].Value.(*ObjectLit)
	typeField := decl.Fields[0]
	require.IsType(t, &TypeObject{}, typeField.Value)

	obj := typeField.Value.(*TypeObject)
	require.Len(t, obj.Fields, 1)
	nested := obj.Fields[0]
	require.NotNil(t, nested.Decl)
	nestedTypeField := nested.Decl.Fields[0]
	require.IsType(t, &TypeAtomic{}, nestedTypeField.Value)
}

func TestValidateInputDeclarationsUsesTypeParserSpans(t *testing.T) {
	src := ubtest.ReadValidFixture(t, "testdata/ub/inputs", "parsed-open-object")
	block := parseInputsBlock(t, src)

	errs := ValidateInputDeclarations(block)
	require.Equal(t, 0, errs.Len(), errs.Error())

	decl := block.Fields[0].Value.(*ObjectLit)
	typeField := decl.Fields[0]
	require.IsType(t, &TypeObject{}, typeField.Value)
	assert.Equal(t, 3, typeField.Value.Span().Start.Line)
	assert.Equal(t, 20, typeField.Value.Span().Start.Column)
}

func constraintsBlock(f *File) (*ArrayLit, bool) {
	if len(f.Body.Fields) == 0 || f.Body.Fields[0].Key.Name != "constraints" {
		return nil, false
	}
	a, ok := f.Body.Fields[0].Value.(*ArrayLit)
	return a, ok
}

func parseConstraintsBlock(t *testing.T, src string) *ArrayLit {
	t.Helper()
	f, err := ParseSource("", []byte(src))
	require.NoError(t, err)
	block, ok := constraintsBlock(f)
	require.True(t, ok, "expected `constraints:` to be an array literal")
	return block
}

func TestValidateConstraintsFixtures(t *testing.T) {
	ubtest.Run(t, "testdata/ub/constraints", func(name string, src []byte) (string, []string) {
		f, err := ParseSource("", src)
		if err != nil {
			return "", []string{err.Error()}
		}
		block, ok := constraintsBlock(f)
		if !ok {
			return "", []string{"fixture must begin with a constraints: array"}
		}
		return "", ValidateConstraints(block).Messages()
	})
}

func namedObjectBlock(f *File, key string) (*ObjectLit, bool) {
	if len(f.Body.Fields) == 0 || f.Body.Fields[0].Key.Name != key {
		return nil, false
	}
	o, ok := f.Body.Fields[0].Value.(*ObjectLit)
	return o, ok
}

// objectBlockDriver runs the named top-level object block through validate,
// reporting its diagnostics as the fixture's expected errors.
func objectBlockDriver(key string, validate func(*ObjectLit) *ErrorList) ubtest.Driver {
	return func(name string, src []byte) (string, []string) {
		f, err := ParseSource("", src)
		if err != nil {
			return "", []string{err.Error()}
		}
		o, ok := namedObjectBlock(f, key)
		if !ok {
			return "", []string{"fixture must begin with a " + key + ": object"}
		}
		return "", validate(o).Messages()
	}
}

func TestValidateImportsFixtures(t *testing.T) {
	ubtest.Run(t, "testdata/ub/imports", objectBlockDriver("imports", ValidateImports))
}

func TestValidateOutputsFixtures(t *testing.T) {
	ubtest.Run(t, "testdata/ub/outputs", objectBlockDriver("outputs", ValidateOutputs))
}

func TestValidateLocalsFixtures(t *testing.T) {
	ubtest.Run(t, "testdata/ub/locals", objectBlockDriver("locals", ValidateLocals))
}

func TestValidateInputsFixtures(t *testing.T) {
	ubtest.Run(t, "testdata/ub/inputs", objectBlockDriver("inputs", ValidateInputDeclarations))
}

// TestValidateConstraintReferencesFixtures checks that constraint fields
// resolve against the declared inputs of the same fixture.
func TestValidateConstraintReferencesFixtures(t *testing.T) {
	ubtest.Run(t, "testdata/ub/constraint-refs", func(name string, src []byte) (string, []string) {
		f, err := ParseSource("", src)
		if err != nil {
			return "", []string{err.Error()}
		}
		var inputs *ObjectLit
		var constraints *ArrayLit
		for _, fld := range f.Body.Fields {
			switch fld.Key.Name {
			case "inputs":
				inputs, _ = fld.Value.(*ObjectLit)
			case "constraints":
				constraints, _ = fld.Value.(*ArrayLit)
			}
		}
		if inputs == nil || constraints == nil {
			return "", []string{"fixture needs inputs: and constraints: blocks"}
		}
		return "", ValidateConstraintReferences(constraints, inputs).Strings()
	})
}
