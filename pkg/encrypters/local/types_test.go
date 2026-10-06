package local

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/sdk/cfg"
	sdkencrypt "github.com/cloudboss/unobin/pkg/sdk/encrypt"
)

func TestTypesProvideLocalEncryption(t *testing.T) {
	t.Setenv("UB_LOCAL_TYPE_TEST_KEY", base64.StdEncoding.EncodeToString(make([]byte, 32)))
	types := Types()
	require.Len(t, types, 2)
	require.Equal(t, "env-key", types[0].Name)
	require.Equal(t, "noop", types[1].Name)
	config, err := cfg.Decode(types[0].Configuration, map[string]any{
		"env-var": "UB_LOCAL_TYPE_TEST_KEY",
	})
	require.NoError(t, err)
	for i, registered := range types {
		t.Run(registered.Name, func(t *testing.T) {
			var configuration any
			if i == 0 {
				configuration = config
			}
			encrypter, err := registered.New(configuration, nil)
			require.NoError(t, err)
			plaintext := []byte("local state")
			sealed, err := encrypter.Encrypt(plaintext)
			require.NoError(t, err)
			opened, err := encrypter.Decrypt(sealed)
			require.NoError(t, err)
			require.Equal(t, plaintext, opened)
			want := sdkencrypt.Description{KeySource: registered.Name}
			if i == 0 {
				want.Config = map[string]any{"env-var": "UB_LOCAL_TYPE_TEST_KEY"}
			}
			require.Equal(t, want, encrypter.Describe())
		})
	}
}
