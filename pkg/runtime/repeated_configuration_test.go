package runtime

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRepeatedCompositeConfigurationsRemainIndependent(t *testing.T) {
	for _, category := range []Category{NodeResource, NodeDataSource, NodeAction} {
		for _, nested := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/nested=%t", category, nested), func(t *testing.T) {
				library := configurationScopeLibrary(t, category, nested)
				libs := map[string]*Library{"wrapper": library}
				source := fmt.Sprintf(configurationScopeFixture(t, "repeated-factory.ub.tmpl"),
					configurationCategoryBlock(category))
				exec := scopedConfigurationExecutor(t, source, libs)
				exec.Inputs = map[string]any{
					"endpoints": map[string]any{"a/b": "A", "quote'key": "B"},
				}
				_, err := planAndApply(exec)
				require.NoError(t, err)
				snapshot, err := exec.Store.Current()
				require.NoError(t, err)
				got := map[string]any{}
				for _, entry := range snapshot.Entries {
					got[entry.Address] = entry.Outputs["endpoint"]
				}
				want := map[string]any{}
				for key, endpoint := range exec.Inputs["endpoints"].(map[string]any) {
					address := instanceAddress(string(category)+".apps", key)
					want[address] = endpoint
					if nested {
						address += "/" + string(category) + ".child"
						want[address] = endpoint
					}
					want[address+"/"+string(category)+".this"] = endpoint
				}
				require.Equal(t, want, got)
				_, err = planAndApply(exec)
				require.NoError(t, err)
			})
		}
	}
}

func TestRepeatedCompositeConfigurationSurvivesRemoval(t *testing.T) {
	for _, operation := range []string{"refresh", "orphan", "destroy"} {
		t.Run(operation, func(t *testing.T) {
			var reads, deletes []string
			library := configurationScopeLibrary(t, NodeResource, true)
			configuredPrimitive(library).Resources =
				configuredLibrariesRecording(&reads, &deletes)["fix"].Resources
			libs := map[string]*Library{"wrapper": library}
			source := fmt.Sprintf(configurationScopeFixture(t, "repeated-factory.ub.tmpl"), "resources")
			first := scopedConfigurationExecutor(t, source, libs)
			first.Inputs = map[string]any{"endpoints": map[string]any{"a/b": "A", "b": "B"}}
			applyOnce(t, first)

			fresh := configurationTestExecutor(t, source, libs)
			fresh.Store, fresh.Factory = first.Store, first.Factory
			fresh.Inputs = map[string]any{"endpoints": map[string]any{"a/b": "A"}}
			fresh.Parallelism = 1
			switch operation {
			case "refresh":
				_, err := fresh.Refresh(context.Background())
				require.NoError(t, err)
				slices.Sort(reads)
				require.Equal(t, []string{"A", "B"}, reads)
			case "orphan", "destroy":
				fresh.Destroy = operation == "destroy"
				_, err := planAndApply(fresh)
				require.NoError(t, err)
				want := []string{"B"}
				if operation == "destroy" {
					want = []string{"A", "B"}
				}
				slices.Sort(deletes)
				require.Equal(t, want, deletes)
			}
		})
	}
}

func TestRepeatedCompositeConfigErrorNamesInstance(t *testing.T) {
	library := configurationScopeLibrary(t, NodeResource, false)
	configuredPrimitive(library).Schema = plainEndpointSchema(false, true)
	source := fmt.Sprintf(configurationScopeFixture(t, "repeated-factory.ub.tmpl"), "resources")
	exec := scopedConfigurationExecutor(t, source, map[string]*Library{"wrapper": library})
	exec.Inputs = map[string]any{"endpoints": map[string]any{"bad": ""}}
	_, err := exec.Plan(context.Background())
	require.ErrorContains(t, err, "resource.apps['bad']/library-config.fix")
}

func TestConfigurationStepsUseInstanceDependencies(t *testing.T) {
	boundary := "resource.apps"
	config := boundary + "/library-config.fix"
	leaf := boundary + "/resource.this"
	dag := &DAG{
		Nodes: map[string]*Node{
			boundary: {Address: boundary, Kind: NodeResource},
			config:   {Address: config, Kind: NodeLibraryConfig},
			leaf:     {Address: leaf, Kind: NodeResource},
		},
		Edges: map[string][]string{leaf: {config}, boundary: {config, leaf}},
	}
	var addresses []string
	want := map[string][]string{}
	for _, key := range []string{"a/b", "quote'key"} {
		owner := instanceAddress(boundary, key)
		configuration := owner + "/library-config.fix"
		resource := owner + "/resource.this"
		addresses = append(addresses, configuration, resource, owner)
		want[configuration] = []string{resource, owner}
		want[resource] = []string{owner}
		require.Equal(t, config, declarationAddress(configuration))
	}
	graph := buildStepGraphFromAddresses(addresses, dag)
	require.Equal(t, want, graph.dependents)
	for _, key := range []string{"a/b", "quote'key"} {
		owner := instanceAddress(boundary, key)
		require.Equal(t, 0, graph.indegree[owner+"/library-config.fix"])
		require.Equal(t, 1, graph.indegree[owner+"/resource.this"])
		require.Equal(t, 2, graph.indegree[owner])
	}
}
