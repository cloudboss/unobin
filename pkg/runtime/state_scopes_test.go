package runtime

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func configuredPrimitive(library *Library) *Library {
	composite := library.ResourceComposites["app"]
	if inner := composite.Libraries["inner"]; inner != nil {
		return configuredPrimitive(inner)
	}
	return composite.Libraries["fix"]
}

func TestPriorCompositeConfigurationsUseEnclosingScopes(t *testing.T) {
	for _, nested := range []bool{false, true} {
		for _, operation := range []string{"plan", "refresh", "destroy", "orphan"} {
			t.Run(fmt.Sprintf("%s/nested=%t", operation, nested), func(t *testing.T) {
				var reads, deletes []string
				library := configurationScopeLibrary(t, NodeResource, nested)
				configuredPrimitive(library).Resources =
					configuredLibrariesRecording(&reads, &deletes)["fix"].Resources
				libs := map[string]*Library{"wrapper": library}
				source := configurationScopeFixture(t, "state-factory.ub")
				first := scopedConfigurationExecutor(t, source, libs)
				first.Inputs = map[string]any{"endpoint": "ROOT"}
				applyOnce(t, first)

				if operation == "orphan" {
					source = strings.ReplaceAll(source, "  b: wrapper.app { endpoint: 'B' }\n", "")
				}
				fresh := configurationTestExecutor(t, source, libs)
				fresh.Store, fresh.Factory = first.Store, first.Factory
				fresh.Inputs = map[string]any{"endpoint": "OTHER"}
				fresh.Parallelism = 1
				switch operation {
				case "plan":
					plan, err := fresh.Plan(context.Background())
					require.NoError(t, err)
					for _, step := range plan.Steps {
						require.Contains(t, []Decision{DecisionNoOp, DecisionEval}, step.Decision)
					}
				case "refresh":
					_, err := fresh.Refresh(context.Background())
					require.NoError(t, err)
				case "destroy", "orphan":
					fresh.Destroy = operation == "destroy"
					_, err := planAndApply(fresh)
					require.NoError(t, err)
				}
				if operation == "destroy" || operation == "orphan" {
					slices.Sort(deletes)
					want := []string{"A", "B"}
					if operation == "orphan" {
						want = []string{"B"}
					}
					require.Equal(t, want, deletes)
				} else {
					slices.Sort(reads)
					require.Equal(t, []string{"A", "B"}, reads)
				}
			})
		}
	}
}

func TestPriorCompositeConfigUsesCurrentForwardedInputs(t *testing.T) {
	var reads []string
	library := configurationScopeLibrary(t, NodeResource, false)
	configuredPrimitive(library).Resources = configuredLibrariesRecording(&reads, nil)["fix"].Resources
	libs := map[string]*Library{"wrapper": library}
	source := configurationScopeFixture(t, "forwarded-factory.ub")
	first := scopedConfigurationExecutor(t, source, libs)
	first.Inputs = map[string]any{"endpoint": "OLD"}
	applyOnce(t, first)

	fresh := configurationTestExecutor(t, source, libs)
	fresh.Store, fresh.Factory = first.Store, first.Factory
	fresh.Inputs = map[string]any{"endpoint": "CURRENT"}
	_, err := fresh.Refresh(context.Background())
	require.NoError(t, err)
	require.Equal(t, []string{"CURRENT"}, reads)
}

func TestPriorCompositeConfigReadsScopedSiblingValues(t *testing.T) {
	for _, destroy := range []bool{false, true} {
		t.Run(fmt.Sprintf("destroy=%t", destroy), func(t *testing.T) {
			var reads, deletes []string
			library := configurationScopeLibrary(t, NodeResource, false)
			body := syntaxResourceComposite(t, "app", configurationScopeFixture(t, "derived-body.ub"))
			body.Libraries = configuredLibrariesRecording(&reads, &deletes)
			library.ResourceComposites["app"] = body
			libs := map[string]*Library{"wrapper": library}
			source := configurationScopeFixture(t, "forwarded-factory.ub")
			first := scopedConfigurationExecutor(t, source, libs)
			first.Inputs = map[string]any{"endpoint": "CHILD"}
			applyOnce(t, first)

			fresh := configurationTestExecutor(t, source, libs)
			fresh.Store, fresh.Factory = first.Store, first.Factory
			fresh.Inputs = first.Inputs
			fresh.Destroy = destroy
			if destroy {
				_, err := planAndApply(fresh)
				require.NoError(t, err)
				require.Equal(t, []string{"CHILD"}, deletes)
			} else {
				_, err := fresh.Refresh(context.Background())
				require.NoError(t, err)
				require.Equal(t, []string{"CHILD"}, reads)
			}
		})
	}
}

func TestStateScopesKeepCompositeInputsPrivate(t *testing.T) {
	library := configurationScopeLibrary(t, NodeResource, false)
	libs := map[string]*Library{"wrapper": library}
	source := configurationScopeFixture(t, "state-factory.ub")
	exec := scopedConfigurationExecutor(t, source, libs)
	exec.Inputs = map[string]any{"endpoint": "ROOT"}
	applyOnce(t, exec)
	snapshot, err := exec.Store.Current()
	require.NoError(t, err)
	for _, entry := range snapshot.Entries {
		if entry.Composite {
			entry.Inputs["private"] = "internal"
		}
	}
	view, err := exec.newStateScopes(snapshot, exec.Inputs)
	require.NoError(t, err)
	require.Equal(t, map[string]any{
		"a": map[string]any{"endpoint": "A"},
		"b": map[string]any{"endpoint": "B"},
	}, view.eval.Resources)
	require.Empty(t, view.eval.Data)
	require.Empty(t, view.eval.Actions)
}
