package factorycli

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	localbackend "github.com/cloudboss/unobin/pkg/backends/local"
	"github.com/cloudboss/unobin/pkg/diagnostic"
	"github.com/cloudboss/unobin/pkg/encoding/ub"
	localencrypt "github.com/cloudboss/unobin/pkg/encrypters/local"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
	"github.com/cloudboss/unobin/pkg/sdk/encrypt"
	sdkstate "github.com/cloudboss/unobin/pkg/sdk/state"
)

func TestStateSchemaReportsSelectedTypes(t *testing.T) {
	backend := localbackend.Type()
	backend.Name = "custom"
	backend.Configuration = &cfg.ConfigurationType[any]{New: func() any {
		return &struct {
			Endpoint *struct{ Port int64 }
		}{}
	}}
	var calls int
	constructor := backend.New
	backend.New = func(config any, factory, stack string, enc encrypt.Encrypter) (
		sdkstate.Backend, error,
	) {
		calls++
		return constructor(config, factory, stack, enc)
	}
	root := newRootCmd(Info{FactoryName: "factory", Registries: &Registries{
		Backends: []sdkstate.BackendType{backend}, Encrypters: localencrypt.Types(),
	}})
	root.SetArgs([]string{"schema", "state", "--format", "json"})
	var output bytes.Buffer
	root.SetOut(&output)
	require.NoError(t, root.Execute())
	var got stateSchemaResult
	require.NoError(t, json.Unmarshal(output.Bytes(), &got))
	require.Equal(t, stateSchemaResult{
		Kind: "state-schema", FormatVersion: 1, Factory: factoryIdentity{Name: "factory"},
		Backends: []stateSchemaType{{Name: "custom", Description: backend.Description,
			Configuration: []stateSchemaField{{Name: "endpoint", Type: "object", Optional: true,
				Fields: []stateSchemaField{{Name: "port", Type: "integer", Fields: []stateSchemaField{}}},
			}},
		}},
		Encrypters: []stateSchemaType{
			{Name: "env-key", Description: localencrypt.Types()[0].Description,
				Configuration: []stateSchemaField{{Name: "env-var", Type: "string",
					Fields: []stateSchemaField{}}}},
			{Name: "noop", Description: localencrypt.Types()[1].Description,
				Configuration: []stateSchemaField{}},
		},
		Diagnostics: []diagnostic.Diagnostic{},
	}, got)
	require.Zero(t, calls)
}

func TestStateSchemaFormatsExplicitEmpty(t *testing.T) {
	for _, format := range []string{"json", "unobin", "text"} {
		t.Run(format, func(t *testing.T) {
			root := newRootCmd(Info{FactoryName: "factory", Registries: &Registries{}})
			root.SetArgs([]string{"schema", "state", "--format", format})
			var output bytes.Buffer
			root.SetOut(&output)
			require.NoError(t, root.Execute())
			if format == "text" {
				require.Equal(t, "State backends:\n  None.\n\nEncryption types:\n  None.\n",
					output.String())
				return
			}
			var got stateSchemaResult
			if format == "json" {
				require.NoError(t, json.Unmarshal(output.Bytes(), &got))
			} else {
				require.NoError(t, ub.Unmarshal(output.Bytes(), &got))
			}
			require.Equal(t, stateSchemaResult{
				Kind: "state-schema", FormatVersion: 1,
				Factory:  factoryIdentity{Name: "factory"},
				Backends: []stateSchemaType{}, Encrypters: []stateSchemaType{},
				Diagnostics: []diagnostic.Diagnostic{},
			}, got)
		})
	}
}

func TestStateSchemaPropagatesWriterFailure(t *testing.T) {
	root := newRootCmd(Info{FactoryName: "factory"})
	root.SetArgs([]string{"schema", "state"})
	root.SetOut(&planFailureWriter{})
	require.EqualError(t, root.Execute(), "writer failed")
}
