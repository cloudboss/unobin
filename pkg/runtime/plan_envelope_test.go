package runtime

import (
	"encoding/json"
	"testing"

	"github.com/cloudboss/unobin/pkg/sdk/encrypt"
	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// reversingEncrypter is a deterministic toy encrypter for envelope
// tests. It reverses the input bytes; round-tripping recovers the
// original. The body is observably non-JSON so the envelope's
// plaintext/ciphertext distinction is testable.
type reversingEncrypter struct{}

func (reversingEncrypter) Encrypt(b []byte) ([]byte, error) { return reverse(b), nil }
func (reversingEncrypter) Decrypt(b []byte) ([]byte, error) { return reverse(b), nil }

func reverse(b []byte) []byte {
	out := make([]byte, len(b))
	for i, x := range b {
		out[len(b)-1-i] = x
	}
	return out
}

func samplePlan() *Plan {
	return &Plan{
		Factory: state.FactoryInfo{Name: "demo", Version: "v0.1.0", ContentRevision: "abc"},
		Stack:   "default",
	}
}

func TestSealPlanOpenPlanRoundTrip(t *testing.T) {
	encRef := &StateRef{Alias: "aws", Name: "kms", Body: map[string]any{"key-id": "alias/p"}}
	sealed, err := SealPlan(samplePlan(), encRef, reversingEncrypter{})
	require.NoError(t, err)

	pf, err := OpenPlan(sealed, func(ref *StateRef) (encrypt.Encrypter, error) {
		require.NotNil(t, ref, "envelope should carry the encrypter ref")
		assert.Equal(t, "aws", ref.Alias)
		assert.Equal(t, "kms", ref.Name)
		assert.Equal(t, "alias/p", ref.Body["key-id"])
		return reversingEncrypter{}, nil
	})
	require.NoError(t, err)
	require.Equal(t, "demo", pf.Factory.Name)
	require.Equal(t, "default", pf.Stack)
	require.Equal(t, PlanFormatVersion, pf.FormatVersion)
}

func TestSealPlanOmitsEncrypterRefWhenNil(t *testing.T) {
	sealed, err := SealPlan(samplePlan(), nil, reversingEncrypter{})
	require.NoError(t, err)
	var env PlanEnvelope
	require.NoError(t, json.Unmarshal(sealed, &env))
	require.Nil(t, env.Encrypter)

	_, err = OpenPlan(sealed, func(ref *StateRef) (encrypt.Encrypter, error) {
		assert.Nil(t, ref, "resolver should receive nil ref when none is in the envelope")
		return reversingEncrypter{}, nil
	})
	require.NoError(t, err)
}

func TestOpenPlanRejectsUnknownEnvelopeVersion(t *testing.T) {
	env := PlanEnvelope{EnvelopeVersion: 99, Ciphertext: []byte("ignored")}
	body, err := json.Marshal(env)
	require.NoError(t, err)
	_, err = OpenPlan(body, func(*StateRef) (encrypt.Encrypter, error) {
		return reversingEncrypter{}, nil
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "envelope-version 99")
}
