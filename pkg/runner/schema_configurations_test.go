package runner

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
)

const configuredFactorySrc = `
factory: {
  inputs: { region: { type: string } }
  configurations: { admin: aws { region: var.region } }
  resources: {
    a: aws.thing {}
  }
}
`

func configuredSchemaInfo() Info {
	return Info{
		FactoryName:     "test-stack",
		FactoryVersion:  "v0.1.0",
		ContentRevision: "abcdef",
		FactoryBody:     configuredFactorySrc,
		Libraries:       map[string]*runtime.Library{"aws": awsModuleWithConfig()},
	}
}

func TestSchemaShowsConfigurations(t *testing.T) {
	out, err := runRoot(t, configuredSchemaInfo(), "schema")
	require.NoError(t, err)
	require.Contains(t, out, "configurations:")
	require.Contains(t, out, "  aws:")
	require.Contains(t, out, "    internal: admin")
	require.Contains(t, out, "    needed from stack file: default")
	require.Contains(t, out, "      region: string")
	require.Contains(t, out, "      profile: optional(string)")
	require.Contains(t, out, "      assume-role: optional(object)")
	require.Contains(t, out, "        role-arn: string")
	require.Contains(t, out, "        external-id: optional(string)")
}

func TestSchemaTemplateScaffoldsOwedConfigurations(t *testing.T) {
	out, err := runRoot(t, configuredSchemaInfo(), "schema", "template")
	require.NoError(t, err)
	require.Contains(t, out, "configurations: {")
	require.Contains(t, out, "      aws {")
	require.NotContains(t, out, "      east2: aws {")
	require.NotContains(t, out, "      admin: aws {")
	require.NotContains(t, out, "aws.default")
	require.Contains(t, out, "        region:  ''  # type: string")
	require.Contains(t, out, "        profile: ''  # type: optional(string)")
	require.Contains(t, out, "        # type: optional(object)")
	require.Contains(t, out, "        assume-role: {")
	require.Contains(t, out, "          role-arn:    ''  # type: string")
	require.Contains(t, out, "          external-id: ''  # type: optional(string)")
}

func TestSchemaTemplateScaffoldsNamedOwedConfigurationsAsSelectorBodies(t *testing.T) {
	dag := &runtime.DAG{Nodes: map[string]*runtime.Node{
		"resource.aws.thing.b": {
			Address:       "resource.aws.thing.b",
			Kind:          runtime.NodeResource,
			Alias:         "aws",
			Type:          "thing",
			Name:          "b",
			Configuration: "east2",
		},
	}}
	info := Info{Libraries: map[string]*runtime.Library{"aws": awsModuleWithConfig()}}

	var out bytes.Buffer
	renderConfigurationsTemplate(&out, &lang.File{}, dag, info)

	got := out.String()
	require.Contains(t, got, "configurations: {\n")
	require.Contains(t, got, "east2: aws {\n")
	require.NotContains(t, got, "aws.east2")
}
