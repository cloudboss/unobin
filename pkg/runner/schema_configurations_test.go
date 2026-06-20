package runner

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/lang/syntax"
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

func TestSchemaHidesConfigurations(t *testing.T) {
	out, err := runRoot(t, configuredSchemaInfo(), "schema")
	require.NoError(t, err)
	require.Contains(t, out, "region: string")
	require.NotContains(t, out, "configurations:")
	require.NotContains(t, out, "needed from stack file")
}

func TestSchemaTemplateOmitsOwedConfigurations(t *testing.T) {
	out, err := runRoot(t, configuredSchemaInfo(), "schema", "template")
	require.NoError(t, err)
	require.Contains(t, out, "inputs: {")
	require.Contains(t, out, "region: ''  # type: string")
	require.NotContains(t, out, "configurations: {")
	require.NotContains(t, out, "aws {")
	require.NotContains(t, out, "admin: aws {")
}

func TestSchemaTemplateScaffoldsNamedOwedConfigurationsAsSelectorBodies(t *testing.T) {
	dag := &runtime.DAG{Nodes: map[string]*runtime.Node{
		"resource.aws.thing.b": {
			Address:       "resource.aws.thing.b",
			Kind:          runtime.NodeResource,
			Alias:         "aws",
			Type:          "thing",
			Name:          "b",
			Configuration: runtime.ConfigRef{Alias: "aws", Name: "east2"},
		},
	}}
	info := Info{Libraries: map[string]*runtime.Library{"aws": awsModuleWithConfig()}}

	body := syntax.FactoryBody{}
	parsed := &parsedFactory{syntaxBody: &body, dag: dag}

	var out bytes.Buffer
	renderConfigurationsTemplate(&out, parsed, info)

	got := out.String()
	require.Contains(t, got, "configurations: {\n")
	require.Contains(t, got, "east2: aws {\n")
	require.NotContains(t, got, "aws.east2")
}

func TestSchemaTemplateUsesTypedFactoryInputs(t *testing.T) {
	parsed := typedOnlyParsedFactory(t, `factory: {
  inputs: {
    message: { type: string, description: 'Text to write' }
  }
}`,
		nil)

	var out bytes.Buffer
	renderInputsTemplate(&out, parsed)

	got := out.String()
	require.Contains(t, got, "inputs: {\n")
	require.Contains(t, got, "# Text to write\n")
	require.Contains(t, got, "message: ''  # type: string\n")
}

func TestSchemaOutputUsesTypedFactoryOutputs(t *testing.T) {
	parsed := typedOnlyParsedFactory(t, `factory: {
  outputs: {
    secret: { value: 'x', description: 'Hidden', @sensitive: true }
  }
}`,
		nil)

	var out bytes.Buffer
	printOutputSchema(&out, parsed)

	got := out.String()
	require.Contains(t, got, "outputs:\n")
	require.Contains(t, got, "secret (sensitive)  -- Hidden\n")
}

func TestSchemaTemplateUsesTypedInternalConfigurations(t *testing.T) {
	info := Info{Libraries: map[string]*runtime.Library{"aws": awsModuleWithConfig()}}
	parsed := typedOnlyParsedFactory(t, `factory: {
  configurations: { admin: aws {} }
  resources: {
    one: aws.thing { @configuration: configuration.admin }
    two: aws.thing {}
  }
}`,
		info.Libraries)

	var out bytes.Buffer
	renderConfigurationsTemplate(&out, parsed, info)

	got := out.String()
	require.Contains(t, got, "configurations: {\n")
	require.Contains(t, got, "aws {\n")
	require.NotContains(t, got, "admin: aws {\n")
}

func TestRootSensitiveOutputsUsesTypedFactoryOutputs(t *testing.T) {
	parsed := typedOnlyParsedFactory(t, `factory: {
  outputs: {
    secret: { value: 'x', @sensitive: true }
    plain:  { value: 'y' }
  }
}`,
		nil)

	sensitive := rootSensitiveOutputs(parsed)
	require.True(t, sensitive["secret"])
	require.False(t, sensitive["plain"])
}

func typedOnlyParsedFactory(
	t *testing.T,
	src string,
	libs map[string]*runtime.Library,
) *parsedFactory {
	t.Helper()
	f, err := syntax.ParseSource("factory.ub", []byte(src))
	require.NoError(t, err)
	require.Equal(t, syntax.FileFactory, f.Kind)
	require.NotNil(t, f.Factory)
	return &parsedFactory{
		syntaxBody: &f.Factory.Body,
		dag:        runtime.BuildSyntaxDAG(f.Factory.Body, libs),
	}
}
