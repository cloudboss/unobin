package runtime

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/sdk/state"
)

// A resource whose body declares no inputs applies cleanly: the empty
// input set written to the plan file and the empty set evaluated live
// are the same premise, whatever nil-ness each side has.
func TestApplyAcceptsEmptyInputs(t *testing.T) {
	libs := map[string]*Library{
		"core": {
			Name: "core",
			Resources: map[string]ResourceRegistration{
				"thing": MakeResource[trackedResource, any, any](),
			},
		},
	}
	dag, syntaxSource := syntaxDAGAndBody(t,
		ubtest.ReadValidFixture(t, "testdata/ub/apply-empty-inputs", "resource"), libs)
	exec := &Executor{
		DAG:          dag,
		SyntaxSource: syntaxSource,
		Libraries:    libs,
		Store:        newStateStore(t),
		Factory:      state.FactoryInfo{Name: "t", Version: "v0", ContentRevision: "c0"},
	}
	res := applyOnce(t, exec)
	require.NotNil(t, res)
}
