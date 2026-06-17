package runtime

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
)

func newExecutorForConfigCheck(
	nodes map[string]*Node,
	libs map[string]*Library,
	configurations ConfigTable,
) *Executor {
	return &Executor{
		DAG:            &DAG{Nodes: nodes},
		Libraries:      libs,
		Configurations: configurations,
	}
}

func libraryWithConfig() *Library {
	return &Library{
		Name:          "aws",
		Configuration: &cfg.ConfigurationType{New: func() any { return &struct{}{} }},
	}
}

func TestCheckConfigurationsAcceptsValidLeafAlias(t *testing.T) {
	leaf := &Node{
		Address:       "resource.web",
		Kind:          NodeResource,
		Alias:         "aws",
		Configuration: ConfigRef{Alias: "aws", Name: "east2"},
	}
	e := newExecutorForConfigCheck(
		map[string]*Node{leaf.Address: leaf},
		map[string]*Library{"aws": libraryWithConfig()},
		ConfigTable{
			{Alias: "aws", Name: "default"}: "x",
			{Alias: "aws", Name: "east2"}:   "y",
		},
	)
	require.NoError(t, e.CheckConfigurations())
}

func TestCheckConfigurationsRejectsUnknownLeafAlias(t *testing.T) {
	leaf := &Node{
		Address:       "resource.web",
		Kind:          NodeResource,
		Alias:         "aws",
		Configuration: ConfigRef{Alias: "aws", Name: "ghost"},
	}
	e := newExecutorForConfigCheck(
		map[string]*Node{leaf.Address: leaf},
		map[string]*Library{"aws": libraryWithConfig()},
		ConfigTable{{Alias: "aws", Name: "default"}: "x"},
	)
	err := e.CheckConfigurations()
	require.Error(t, err)
	require.Contains(t, err.Error(), "@configuration configuration.ghost")
	require.Contains(t, err.Error(), "configuration not declared")
}

func TestCheckConfigurationsRejectsLeafAliasOnModuleWithoutConfig(t *testing.T) {
	leaf := &Node{
		Address:       "action.run",
		Kind:          NodeAction,
		Alias:         "core",
		Configuration: ConfigRef{Alias: "core", Name: "alt"},
	}
	e := newExecutorForConfigCheck(
		map[string]*Node{leaf.Address: leaf},
		map[string]*Library{"core": {Name: "core"}},
		nil,
	)
	err := e.CheckConfigurations()
	require.Error(t, err)
	require.Contains(t, err.Error(), "library declares no configuration")
}

func TestCheckConfigurationsAcceptsValidRemap(t *testing.T) {
	composite := &Node{
		Address:             "resource.east",
		Kind:                NodeResource,
		Alias:               "net",
		CompositeSyntaxBody: &syntax.FactoryBody{},
		ConfigurationsRemap: map[string]ConfigRef{
			"aws": {Alias: "aws", Name: "east2"},
		},
	}
	e := newExecutorForConfigCheck(
		map[string]*Node{composite.Address: composite},
		map[string]*Library{"aws": libraryWithConfig()},
		ConfigTable{
			{Alias: "aws", Name: "default"}: "x",
			{Alias: "aws", Name: "east2"}:   "y",
		},
	)
	require.NoError(t, e.CheckConfigurations())
}

func TestCheckConfigurationsRejectsMismatchedAliasInRemap(t *testing.T) {
	composite := &Node{
		Address:             "resource.east",
		Kind:                NodeResource,
		Alias:               "net",
		CompositeSyntaxBody: &syntax.FactoryBody{},
		ConfigurationsRemap: map[string]ConfigRef{
			"aws": {Alias: "gcp", Name: "east2"},
		},
	}
	e := newExecutorForConfigCheck(
		map[string]*Node{composite.Address: composite},
		map[string]*Library{"aws": libraryWithConfig()},
		ConfigTable{{Alias: "aws", Name: "default"}: "x"},
	)
	err := e.CheckConfigurations()
	require.Error(t, err)
	require.Contains(t, err.Error(), "@configurations.aws")
	require.Contains(t, err.Error(), `import "gcp" must match the key`)
}

func TestCheckConfigurationsRejectsMissingAliasInRemap(t *testing.T) {
	composite := &Node{
		Address:             "resource.east",
		Kind:                NodeResource,
		Alias:               "net",
		CompositeSyntaxBody: &syntax.FactoryBody{},
		ConfigurationsRemap: map[string]ConfigRef{
			"aws": {Alias: "aws", Name: "ghost"},
		},
	}
	e := newExecutorForConfigCheck(
		map[string]*Node{composite.Address: composite},
		map[string]*Library{"aws": libraryWithConfig()},
		ConfigTable{{Alias: "aws", Name: "default"}: "x"},
	)
	err := e.CheckConfigurations()
	require.Error(t, err)
	require.Contains(t, err.Error(), "configuration.ghost not declared")
}

func TestCheckConfigurationsReportsMultipleErrorsAtOnce(t *testing.T) {
	leaf := &Node{
		Address:       "resource.web",
		Kind:          NodeResource,
		Alias:         "aws",
		Configuration: ConfigRef{Alias: "aws", Name: "ghost"},
	}
	composite := &Node{
		Address:             "resource.east",
		Kind:                NodeResource,
		Alias:               "net",
		CompositeSyntaxBody: &syntax.FactoryBody{},
		ConfigurationsRemap: map[string]ConfigRef{
			"aws": {Alias: "gcp", Name: "east2"},
		},
	}
	e := newExecutorForConfigCheck(
		map[string]*Node{
			leaf.Address:      leaf,
			composite.Address: composite,
		},
		map[string]*Library{"aws": libraryWithConfig()},
		ConfigTable{{Alias: "aws", Name: "default"}: "x"},
	)
	err := e.CheckConfigurations()
	require.Error(t, err)
	require.Contains(t, err.Error(), "@configuration configuration.ghost")
	require.Contains(t, err.Error(), "@configurations.aws")
}

func TestCheckConfigurationsRequiresImplicitDefault(t *testing.T) {
	leaf := &Node{
		Address: "resource.web",
		Kind:    NodeResource,
		Alias:   "aws",
	}
	e := newExecutorForConfigCheck(
		map[string]*Node{leaf.Address: leaf},
		map[string]*Library{"aws": libraryWithConfig()},
		nil,
	)
	err := e.CheckConfigurations()
	require.Error(t, err)
	require.Equal(t,
		`resource.web: library "aws" requires a configuration; `+
			`add aws { ... } under stack.factory.configurations `+
			`or configurations in the factory`,
		err.Error())
}

func TestCheckConfigurationsAcceptsInternalDefault(t *testing.T) {
	leaf := &Node{
		Address: "resource.web",
		Kind:    NodeResource,
		Alias:   "aws",
	}
	internal := &Node{
		Address: "default-configuration.aws",
		Kind:    NodeConfiguration,
		Alias:   "aws",
		Name:    "default",
	}
	e := newExecutorForConfigCheck(
		map[string]*Node{leaf.Address: leaf, internal.Address: internal},
		map[string]*Library{"aws": libraryWithConfig()},
		nil,
	)
	require.NoError(t, e.CheckConfigurations())
}
