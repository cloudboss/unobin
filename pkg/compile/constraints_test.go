package compile

import (
	"testing"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	ubruntime "github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/require"
)

func TestConstraintsFromSchema(t *testing.T) {
	schema := &ubruntime.LibrarySchema{
		Resources: map[string]*ubruntime.TypeSchema{
			"vpc": {Constraints: []lang.ConstraintSpec{
				{Kind: "exactly-one-of", Fields: []string{"cidr-block", "cidr-blocks"}},
			}},
			"subnet": {}, // no constraints: omitted from the result
		},
		DataSources: map[string]*ubruntime.TypeSchema{
			"ami": {Constraints: []lang.ConstraintSpec{
				{Kind: "predicate", When: "true", Require: "(var.owner != null)"},
			}},
		},
		Actions: map[string]*ubruntime.TypeSchema{
			"run": {}, // no constraints: omitted
		},
	}
	got := constraintsFromSchema(schema)
	require.Equal(t, map[string][]lang.ConstraintSpec{
		"resource.vpc": {{Kind: "exactly-one-of", Fields: []string{"cidr-block", "cidr-blocks"}}},
		"data.ami":     {{Kind: "predicate", When: "true", Require: "(var.owner != null)"}},
	}, got)
}

func TestConstraintsFromSchemaEmpty(t *testing.T) {
	require.Nil(t, constraintsFromSchema(nil))
	require.Nil(t, constraintsFromSchema(&ubruntime.LibrarySchema{}))
}

func TestUsedLibraryTypes(t *testing.T) {
	f, err := lang.ParseSource("factory.ub", []byte(`
inputs: { path: { type: string } }
resources: {
  aws.vpc.main: { cidr-block: '10.0.0.0/16' }
  aws.subnet.a: { vpc-id: resource.aws.vpc.main.id }
}
data:    { aws.ami.ubuntu: { most-recent: true } }
actions: { core.command.hi: { argv: ['echo'] } }
`))
	require.NoError(t, err)
	require.Equal(t, map[string]map[string]bool{
		"aws":  {"resource.vpc": true, "resource.subnet": true, "data.ami": true},
		"core": {"action.command": true},
	}, usedLibraryTypes(f))
}

func TestUsedLibraryTypesNoDeclarations(t *testing.T) {
	require.Equal(t, map[string]map[string]bool{}, usedLibraryTypes(nil))
	f, err := lang.ParseSource("factory.ub", []byte("inputs: { x: { type: string } }\n"))
	require.NoError(t, err)
	require.Equal(t, map[string]map[string]bool{}, usedLibraryTypes(f))
}

func TestUsedSyntaxLibraryTypes(t *testing.T) {
	f, err := syntax.ParseSource("factory.ub", []byte(`
factory: {
  inputs: { path: { type: string } }
  resources: {
    main: aws.vpc { cidr-block: '10.0.0.0/16' }
    a: aws.subnet { vpc-id: resource.main.id }
  }
  data:    { ubuntu: aws.ami { most-recent: true } }
  actions: { hi: core.command { argv: ['echo'] } }
}
`))
	require.NoError(t, err)
	require.Equal(t, map[string]map[string]bool{
		"aws":  {"resource.vpc": true, "resource.subnet": true, "data.ami": true},
		"core": {"action.command": true},
	}, usedSyntaxLibraryTypes(f.Factory.Body))
}

func TestPruneUnusedSpecs(t *testing.T) {
	specs := map[string]map[string][]lang.ConstraintSpec{
		"aws": {
			"resource.vpc":    {{Kind: "exactly-one-of"}},
			"resource.subnet": {{Kind: "predicate"}},
			"data.ami":        {{Kind: "predicate"}},
		},
		"unused": {
			"resource.thing": {{Kind: "predicate"}},
		},
	}
	pruneUnusedSpecs(specs, map[string]map[string]bool{
		"aws": {"resource.vpc": true, "data.ami": true},
	})
	require.Equal(t, map[string]map[string][]lang.ConstraintSpec{
		"aws": {
			"resource.vpc": {{Kind: "exactly-one-of"}},
			"data.ami":     {{Kind: "predicate"}},
		},
	}, specs)
}

func TestKeepUsedTypes(t *testing.T) {
	m := map[string][]lang.ConstraintSpec{
		"resource.vpc": {{Kind: "exactly-one-of"}},
		"data.ami":     {{Kind: "predicate"}},
	}
	require.Equal(t, map[string][]lang.ConstraintSpec{
		"resource.vpc": {{Kind: "exactly-one-of"}},
	}, keepUsedTypes(m, map[string]bool{"resource.vpc": true}))
	require.Nil(t, keepUsedTypes(m, map[string]bool{"resource.absent": true}))
	require.Nil(t, keepUsedTypes(m, nil))
}
