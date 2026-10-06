package factorycli

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/cmdout"
	"github.com/cloudboss/unobin/internal/ubtest"
	localbackend "github.com/cloudboss/unobin/pkg/backends/local"
	localencrypt "github.com/cloudboss/unobin/pkg/encrypters/local"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
	sdkencrypt "github.com/cloudboss/unobin/pkg/sdk/encrypt"
	sdkstate "github.com/cloudboss/unobin/pkg/sdk/state"
)

func TestCommandsUseSelectedRegistries(t *testing.T) {
	factory := ubtest.ReadValidFixture(t, "testdata/ub/assets-runner", "empty-factory")
	stack := parseStateConfigFixture(t, "backend-encryption")
	stack.stack.State.Selector.Name = "custom"
	directory := t.TempDir()
	var calls int
	backend := localbackend.Type()
	constructor := backend.New
	backend.Name = "custom"
	backend.New = func(config any, factory, stack string, enc sdkencrypt.Encrypter) (
		sdkstate.Backend, error,
	) {
		calls++
		require.Equal(t, "/tmp/state", config.(*localbackend.Config).Path)
		return constructor(&localbackend.Config{Path: directory}, factory, stack, enc)
	}
	info := testInfo(t, factory)
	info.Registries = &Registries{
		Backends: []sdkstate.BackendType{backend}, Encrypters: localencrypt.Types(),
	}
	require.NoError(t, validateStack(info, stack, "dev.ub"))
	require.Zero(t, calls)
	store, err := loadStore(info, stack, "dev.ub", "dev", localencrypt.Noop{})
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	_, err = store.Current()
	require.ErrorIs(t, err, sdkstate.ErrNoCurrent)
}

func TestInvalidSelectionFailsBeforeConstruction(t *testing.T) {
	factory := ubtest.ReadValidFixture(t, "testdata/ub/plan-command", "factory")
	stack := parseStateConfigFixture(t, "backend-encryption")
	var calls int
	enc := localencrypt.Types()[1]
	enc.New = func(_ any, _ map[string]any) (sdkencrypt.Encrypter, error) {
		calls++
		return localencrypt.Noop{}, nil
	}
	info := testInfo(t, factory)
	info.Registries = &Registries{Encrypters: []sdkencrypt.EncrypterType{enc}}
	_, err := loadEncrypter(info, stack, "dev.ub")
	require.ErrorContains(t, err, `state: no backend named "local"`)
	require.Zero(t, calls)
}

func TestRegistrationErrorUsesMachineOutput(t *testing.T) {
	backend := localbackend.Type()
	info := Info{FactoryName: "factory", Registries: &Registries{
		Backends: []sdkstate.BackendType{backend, backend},
	}}
	command := newRootCmd(info)
	command.SetArgs([]string{"version", "--format", "json"})
	var output bytes.Buffer
	command.SetOut(&output)
	err := command.Execute()
	require.ErrorContains(t, err, `duplicate backend "local"`)
	require.True(t, cmdout.IsReported(err))
	require.JSONEq(t, `{"kind":"command-error","format-version":1,"command":"version",
		"code":"unobin.command.failed","message":"version failed","files":[],
		"diagnostics":[{"code":"unobin.error","severity":"error",
		"message":"state: duplicate backend \"local\""}]}`, output.String())
}

func TestRegistriesRejectInvalidConfiguration(t *testing.T) {
	backend := localbackend.Type()
	backend.Configuration = &cfg.ConfigurationType[any]{New: func() any { return "invalid" }}
	_, err := (&Registries{Backends: []sdkstate.BackendType{backend}}).build()
	require.ErrorContains(t, err, "state: backend \"local\":")
	require.ErrorContains(t, err, "must return a pointer to a struct")
}

func TestMissingEncryptionDefaults(t *testing.T) {
	for _, key := range []string{"", "configured"} {
		t.Run(key, func(t *testing.T) {
			t.Setenv(defaultKeyEnvVar, key)
			info := Info{Registries: &Registries{}}
			_, err := loadEncrypter(info, nil, "")
			name := "noop"
			if key != "" {
				name = "env-key"
			}
			require.ErrorContains(t, err, "encryption: no key-source named \""+name+"\"")
		})
	}
}
