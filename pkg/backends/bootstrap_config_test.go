package backends

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/sdk/cfg"
)

func TestBootstrapConfigurationActivation(t *testing.T) {
	for _, backend := range []string{S3Name, GCSName} {
		t.Run(backend, func(t *testing.T) {
			tests := []struct {
				name    string
				present bool
				value   any
				enabled bool
			}{
				{name: "omitted"},
				{name: "null", present: true},
				{name: "empty", present: true, value: map[string]any{}},
				{name: "settings", present: true, enabled: true},
			}
			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					raw := map[string]any{"bucket": "state"}
					if tt.name == "settings" {
						if backend == S3Name {
							tt.value = map[string]any{"public-access-block": map[string]any{
								"block-public-acls": false,
							}}
						} else {
							tt.value = map[string]any{"versioning": map[string]any{"enabled": false}}
						}
					}
					if tt.present {
						raw["bootstrap"] = tt.value
					}
					value, err := cfg.Decode(Backends()[backend].Configuration, raw)
					require.NoError(t, err)
					switch c := value.(type) {
					case *S3BackendConfig:
						assert.Equal(t, tt.enabled, c.Bootstrap.enabled())
						require.NoError(t, c.Validate())
					case *GCSBackendConfig:
						assert.Equal(t, tt.enabled, c.Bootstrap.enabled())
						require.NoError(t, c.Validate())
					default:
						t.Fatalf("unexpected configuration %T", value)
					}
				})
			}
		})
	}
}

func TestBootstrapRejectsInvalidConfiguration(t *testing.T) {
	tests := []struct {
		name, backend string
		bootstrap     any
	}{
		{name: "local", backend: LocalName, bootstrap: map[string]any{"location": "US"}},
		{name: "s3 boolean", backend: S3Name, bootstrap: true},
		{name: "gcs boolean", backend: GCSName, bootstrap: false},
		{name: "s3 unknown", backend: S3Name, bootstrap: map[string]any{"location": "US"}},
		{name: "gcs unknown", backend: GCSName,
			bootstrap: map[string]any{"versioning": map[string]any{"status": "Enabled"}}},
		{name: "s3 namespace", backend: S3Name,
			bootstrap: map[string]any{"bucket-namespace": "regional"}},
		{name: "s3 versioning", backend: S3Name,
			bootstrap: map[string]any{"versioning": map[string]any{"status": "enabled"}}},
		{name: "s3 ownership", backend: S3Name,
			bootstrap: map[string]any{"ownership-controls": map[string]any{
				"object-ownership": "Owner",
			}}},
		{name: "gcs public access", backend: GCSName,
			bootstrap: map[string]any{"iam-configuration": map[string]any{
				"public-access-prevention": "disabled",
			}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := map[string]any{"bucket": "state", "bootstrap": tt.bootstrap}
			if tt.backend == LocalName {
				delete(raw, "bucket")
				raw["path"] = "state"
			}
			value, err := cfg.Decode(Backends()[tt.backend].Configuration, raw)
			if err == nil {
				err = value.(interface{ Validate() error }).Validate()
			}
			require.Error(t, err)
			assert.Contains(t, strings.ToLower(err.Error()), "bootstrap")
		})
	}
}
