package runner

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefaultRegistriesPreserveBuiltins(t *testing.T) {
	info := withDefaultRegistries(Info{})
	var backends, encrypters []string
	for _, backend := range info.Registries.Backends {
		backends = append(backends, backend.Name)
		require.NotNil(t, backend.New)
	}
	for _, encrypter := range info.Registries.Encrypters {
		encrypters = append(encrypters, encrypter.Name)
		require.NotNil(t, encrypter.New)
	}
	require.Equal(t, []string{"gcs", "local", "s3"}, backends)
	require.Equal(t, []string{"env-key", "gcp-kms", "kms", "noop"}, encrypters)
	selections := &Registries{}
	require.Same(t, selections, withDefaultRegistries(Info{Registries: selections}).Registries)
}

func TestParseFormatCompatibility(t *testing.T) {
	format, err := ParseFormat("json")
	require.NoError(t, err)
	require.Equal(t, FormatJSON, format)
	_, err = ParseFormat("unknown")
	require.EqualError(t, err, "--output: unknown \"unknown\" (want text, json, or unobin)")
}
