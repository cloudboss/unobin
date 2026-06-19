package runtime

import (
	"testing"

	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/stretchr/testify/require"
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
	dag, syntaxSource := syntaxDAGAndBody(t, `resources: { one: core.thing {} }`, libs)
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
