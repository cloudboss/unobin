package runtime

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/sdk/cfg"
)

func newExecutorForConfigCheck(
	nodes map[string]*Node,
	mods map[string]*Module,
	configurations map[string]map[string]any,
) *Executor {
	return &Executor{
		DAG:            &DAG{Nodes: nodes},
		Modules:        mods,
		Configurations: configurations,
	}
}

func moduleWithConfig() *Module {
	return &Module{
		Name:          "aws",
		Configuration: &cfg.ConfigurationType{New: func() any { return &struct{}{} }},
	}
}

func TestCheckConfigurationsAcceptsValidLeafAlias(t *testing.T) {
	leaf := &Node{
		Address:            "resource.aws.instance.web",
		Kind:               NodeResource,
		NS:                 "aws",
		ConfigurationAlias: "east2",
	}
	e := newExecutorForConfigCheck(
		map[string]*Node{leaf.Address: leaf},
		map[string]*Module{"aws": moduleWithConfig()},
		map[string]map[string]any{"aws": {"default": "x", "east2": "y"}},
	)
	require.NoError(t, e.checkConfigurations())
}

func TestCheckConfigurationsRejectsUnknownLeafAlias(t *testing.T) {
	leaf := &Node{
		Address:            "resource.aws.instance.web",
		Kind:               NodeResource,
		NS:                 "aws",
		ConfigurationAlias: "ghost",
	}
	e := newExecutorForConfigCheck(
		map[string]*Node{leaf.Address: leaf},
		map[string]*Module{"aws": moduleWithConfig()},
		map[string]map[string]any{"aws": {"default": "x"}},
	)
	err := e.checkConfigurations()
	require.Error(t, err)
	require.Contains(t, err.Error(), "@configuration aws.ghost")
	require.Contains(t, err.Error(), "alias not declared")
}

func TestCheckConfigurationsRejectsLeafAliasOnModuleWithoutConfig(t *testing.T) {
	leaf := &Node{
		Address:            "action.core.command.run",
		Kind:               NodeAction,
		NS:                 "core",
		ConfigurationAlias: "alt",
	}
	e := newExecutorForConfigCheck(
		map[string]*Node{leaf.Address: leaf},
		map[string]*Module{"core": {Name: "core"}},
		nil,
	)
	err := e.checkConfigurations()
	require.Error(t, err)
	require.Contains(t, err.Error(), "module declares no configuration")
}

func TestCheckConfigurationsAcceptsValidRemap(t *testing.T) {
	composite := &Node{
		Address: "resource.net.cluster.east",
		Kind:    NodeComposite,
		NS:      "net",
		ConfigurationsRemap: map[string]ConfigRef{
			"aws": {NS: "aws", Alias: "east2"},
		},
	}
	e := newExecutorForConfigCheck(
		map[string]*Node{composite.Address: composite},
		map[string]*Module{"aws": moduleWithConfig()},
		map[string]map[string]any{"aws": {"default": "x", "east2": "y"}},
	)
	require.NoError(t, e.checkConfigurations())
}

func TestCheckConfigurationsRejectsMismatchedNamespaceInRemap(t *testing.T) {
	composite := &Node{
		Address: "resource.net.cluster.east",
		Kind:    NodeComposite,
		NS:      "net",
		ConfigurationsRemap: map[string]ConfigRef{
			"aws": {NS: "gcp", Alias: "east2"},
		},
	}
	e := newExecutorForConfigCheck(
		map[string]*Node{composite.Address: composite},
		map[string]*Module{"aws": moduleWithConfig()},
		map[string]map[string]any{"aws": {"default": "x"}},
	)
	err := e.checkConfigurations()
	require.Error(t, err)
	require.Contains(t, err.Error(), "@configurations.aws")
	require.Contains(t, err.Error(), `import "gcp" must match the key`)
}

func TestCheckConfigurationsRejectsMissingAliasInRemap(t *testing.T) {
	composite := &Node{
		Address: "resource.net.cluster.east",
		Kind:    NodeComposite,
		NS:      "net",
		ConfigurationsRemap: map[string]ConfigRef{
			"aws": {NS: "aws", Alias: "ghost"},
		},
	}
	e := newExecutorForConfigCheck(
		map[string]*Node{composite.Address: composite},
		map[string]*Module{"aws": moduleWithConfig()},
		map[string]map[string]any{"aws": {"default": "x"}},
	)
	err := e.checkConfigurations()
	require.Error(t, err)
	require.Contains(t, err.Error(), "alias aws.ghost not declared")
}

func TestCheckConfigurationsReportsMultipleErrorsAtOnce(t *testing.T) {
	leaf := &Node{
		Address:            "resource.aws.instance.web",
		Kind:               NodeResource,
		NS:                 "aws",
		ConfigurationAlias: "ghost",
	}
	composite := &Node{
		Address: "resource.net.cluster.east",
		Kind:    NodeComposite,
		NS:      "net",
		ConfigurationsRemap: map[string]ConfigRef{
			"aws": {NS: "gcp", Alias: "east2"},
		},
	}
	e := newExecutorForConfigCheck(
		map[string]*Node{
			leaf.Address:      leaf,
			composite.Address: composite,
		},
		map[string]*Module{"aws": moduleWithConfig()},
		map[string]map[string]any{"aws": {"default": "x"}},
	)
	err := e.checkConfigurations()
	require.Error(t, err)
	require.Contains(t, err.Error(), "@configuration aws.ghost")
	require.Contains(t, err.Error(), "@configurations.aws")
}
