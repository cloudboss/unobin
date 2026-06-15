package root

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/deps"
)

// writeUnobinConfigurationFixture lays out a factory importing a Go
// library whose configuration struct is awscfg.Configuration from the
// unobin module itself. The unobin replace in the manifest is what
// lets schema extraction read those fields. configBody becomes the
// factory's configurations.aws.default entry.
func writeUnobinConfigurationFixture(t *testing.T, configBody string) string {
	t.Helper()
	setCLIVersion(t, "dev")
	rootDir := findUnobinRoot(t)

	dir := filepath.Join(t.TempDir(), "demo-factory")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.ub"), []byte(`
inputs:  { name: { type: string } }
imports: { aws: 'github.com/example/cloudlib' }
configurations: {
  aws.default: {
`+configBody+`
  }
}
resources: { aws.thing.this: { name: var.name } }
`), 0o644))

	libDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(libDir, "go.mod"),
		[]byte("module github.com/example/cloudlib\n\ngo 1.26\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(libDir, "cloudlib.go"), []byte(`package cloudlib

import (
	"context"

	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
)

func Library() *runtime.Library {
	return &runtime.Library{
		Name:        "cloudlib",
		Description: "Fixture library configured by a type from the unobin module.",
		Configuration: &cfg.ConfigurationType{
			Description: "AWS connection settings.",
			New:         func() any { return &awscfg.Configuration{} },
		},
		Resources: map[string]runtime.ResourceRegistration{
			"thing": runtime.MakeResource[Thing, *ThingOutput](),
		},
	}
}

type Thing struct {
	Name string
}

type ThingOutput struct {
	ID string
}

func (t *Thing) SchemaVersion() int { return 1 }

func (t *Thing) Create(_ context.Context, _ any) (*ThingOutput, error) {
	return &ThingOutput{}, nil
}

func (t *Thing) Read(_ context.Context, _ any, _ *ThingOutput) (*ThingOutput, error) {
	return &ThingOutput{}, nil
}

func (t *Thing) Update(
	_ context.Context, _ any, _ runtime.Prior[Thing, *ThingOutput],
) (*ThingOutput, error) {
	return &ThingOutput{}, nil
}

func (t *Thing) Delete(_ context.Context, _ any, _ *ThingOutput) error {
	return nil
}
`), 0o644))

	require.NoError(t, os.WriteFile(filepath.Join(dir, deps.ManifestFileName),
		[]byte("manifest: {\nrequires: {}\nreplace: {\n"+
			"  'github.com/cloudboss/unobin': '"+rootDir+"'\n"+
			"  'github.com/example/cloudlib': '"+libDir+"'\n"+
			"}\n}\n"), 0o644))

	return filepath.Join(dir, "main.ub")
}

func TestCompileChecksConfigurationFromUnobinPackage(t *testing.T) {
	stackPath := writeUnobinConfigurationFixture(t, `
      assume-role: {
        role-arn:    'arn:aws:iam::123456789012:role/unobin-state'
        external-id: 'unobin-test'
      }
`)
	out, err := runCommand(t, "compile",
		"-p", stackPath, "-o", filepath.Join(t.TempDir(), "build"))
	require.NoError(t, err)
	require.NotContains(t, out, "unchecked")
	require.NotContains(t, out, "not found in reachable source")
}

func TestCompileRejectsUnknownFieldInUnobinConfiguration(t *testing.T) {
	stackPath := writeUnobinConfigurationFixture(t, `
      regoin: 'us-east-1'
`)
	_, err := runCommand(t, "compile",
		"-p", stackPath, "-o", filepath.Join(t.TempDir(), "build"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown field")
	require.Contains(t, err.Error(), "regoin")
}
