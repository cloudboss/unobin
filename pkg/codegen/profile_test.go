package codegen

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/program"
)

func TestGenerateBuildProfile(t *testing.T) {
	for _, profile := range []program.BuildProfile{"", program.BuildProfileFull} {
		out, err := Generate(Input{FactoryName: "factory", BuildProfile: profile})
		require.NoError(t, err)
		require.Contains(t, string(out), `"github.com/cloudboss/unobin/pkg/runner"`)
		require.Contains(t, string(out), "runner.Run(runner.Info{")
	}
	out, err := Generate(Input{FactoryName: "factory", BuildProfile: program.BuildProfileLocal})
	require.NoError(t, err)
	require.Contains(t, string(out), `"github.com/cloudboss/unobin/pkg/factorycli"`)
	require.Contains(t, string(out), "factorycli.Run(factorycli.Info{")
	require.NotContains(t, string(out), `"github.com/cloudboss/unobin/pkg/runner"`)
	out, err = Generate(Input{FactoryName: "factory", BuildProfile: "custom"})
	require.Nil(t, out)
	require.EqualError(t, err, "codegen: unknown build profile \"custom\" (want full or local)")
}
