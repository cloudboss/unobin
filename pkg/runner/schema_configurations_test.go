package runner

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/runtime"
)

const configuredFactorySrc = `
inputs: { region: { type: string } }
configurations: { aws.admin: { region: var.region } }
resources: {
  aws.thing.a: {}
  aws.thing.b: { @configuration: aws.east2 }
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
	require.Contains(t, out, "    needed from stack file: default, east2")
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
	require.Contains(t, out, "      east2: aws {")
	require.NotContains(t, out, "      admin: aws {")
	require.Contains(t, out, "        region:  ''  # type: string")
	require.Contains(t, out, "        profile: ''  # type: optional(string)")
	require.Contains(t, out, "        # type: optional(object)")
	require.Contains(t, out, "        assume-role: {")
	require.Contains(t, out, "          role-arn:    ''  # type: string")
	require.Contains(t, out, "          external-id: ''  # type: optional(string)")
}
