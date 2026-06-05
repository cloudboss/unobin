package compile

import (
	"testing"

	"github.com/cloudboss/unobin/pkg/lang"
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
