package generate

import (
	"bytes"
	"context"
	"testing"

	"github.com/cloudboss/unobin/pkg/gogen"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

type fakeGoLibraryAdapter struct{}

func (fakeGoLibraryAdapter) Name() string { return "demo" }

func (fakeGoLibraryAdapter) FetchResources(
	_ context.Context,
	_ []string,
) ([]gogen.ResourceSchema, error) {
	return []gogen.ResourceSchema{
		{
			GoName:    "Bucket",
			CloudType: "demo_bucket",
		},
	}, nil
}

func (fakeGoLibraryAdapter) FetchDataSources(
	_ context.Context,
	_ []string,
) ([]gogen.DataSourceSchema, error) {
	return []gogen.DataSourceSchema{
		{
			GoName:    "Image",
			CloudType: "demo_image",
		},
		{
			GoName:    "Network",
			CloudType: "demo_network",
		},
	}, nil
}

func (fakeGoLibraryAdapter) FetchConfiguration(
	_ context.Context,
) (*gogen.ConfigurationSchema, error) {
	return nil, nil
}

func TestGenerateGoLibraryReportsResourcesAndDataSources(t *testing.T) {
	prevAdapter := newGoLibraryAdapter
	newGoLibraryAdapter = func(*golibraryConfig) gogen.SchemaAdapter {
		return fakeGoLibraryAdapter{}
	}
	t.Cleanup(func() { newGoLibraryAdapter = prevAdapter })

	prevVersion := CLIVersion
	CLIVersion = func() string { return "dev" }
	t.Cleanup(func() { CLIVersion = prevVersion })

	cmd := &cobra.Command{}
	stderr := &bytes.Buffer{}
	cmd.SetErr(stderr)

	err := runGenerate(cmd, &golibraryConfig{
		from:         "tf",
		provider:     "hashicorp/demo",
		output:       t.TempDir(),
		goModulePath: "example.com/demo",
	})
	require.NoError(t, err)
	require.Contains(t, stderr.String(), "1 resources")
	require.Contains(t, stderr.String(), "2 data sources")
}
