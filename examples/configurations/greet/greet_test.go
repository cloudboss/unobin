package greet

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/sdk/cfg"
)

func TestSayPrependsPrefix(t *testing.T) {
	a := &SayAction{Message: "world"}
	out, err := a.Run(context.Background(), &Configuration{
		Prefix: cfg.String{Value: "hello"},
	})
	require.NoError(t, err)
	require.Equal(t, "hello: world", out.(SayResult).Output)
}

func TestSayErrorsWithoutConfiguration(t *testing.T) {
	a := &SayAction{Message: "world"}
	_, err := a.Run(context.Background(), nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing or wrong configuration")
}

func TestModuleDeclaresConfiguration(t *testing.T) {
	mod := Module()
	require.Equal(t, "greet", mod.Name)
	require.NotNil(t, mod.Configuration)
	require.NoError(t, cfg.ValidateConfigurationType(mod.Configuration))
}
