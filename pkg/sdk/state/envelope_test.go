package state

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/cloudboss/unobin/pkg/sdk/encrypt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// reversingEncrypter reverses its input, so a round trip recovers the
// original. The sealed body is observably not the plaintext, so the
// envelope's plaintext header and encrypted body are distinguishable.
type reversingEncrypter struct{}

func (reversingEncrypter) Encrypt(b []byte) ([]byte, error) { return reverseBytes(b), nil }
func (reversingEncrypter) Decrypt(b []byte) ([]byte, error) { return reverseBytes(b), nil }
func (reversingEncrypter) Describe() encrypt.Description {
	return encrypt.Description{
		KeySource: "reversing",
		Config:    map[string]any{"direction": "backward"},
	}
}

// failingEncrypter stands in for a wrong key: its Decrypt always errors.
// Its description has no configuration.
type failingEncrypter struct{}

func (failingEncrypter) Encrypt(b []byte) ([]byte, error) { return b, nil }
func (failingEncrypter) Decrypt([]byte) ([]byte, error) {
	return nil, errors.New("authentication failed")
}
func (failingEncrypter) Describe() encrypt.Description {
	return encrypt.Description{KeySource: "failing"}
}

func reverseBytes(b []byte) []byte {
	out := make([]byte, len(b))
	for i, x := range b {
		out[len(b)-1-i] = x
	}
	return out
}

func TestSealOpenRoundTrip(t *testing.T) {
	sealed, err := Seal([]byte("the body"), reversingEncrypter{})
	require.NoError(t, err)

	body, err := Open(sealed, func(got *Ref) (encrypt.Encrypter, error) {
		require.NotNil(t, got, "envelope should include the encrypter ref")
		assert.Equal(t, "reversing", got.Name)
		assert.Equal(t, map[string]any{"direction": "backward"}, got.Body)
		return reversingEncrypter{}, nil
	})
	require.NoError(t, err)
	require.Equal(t, []byte("the body"), body)
}

func TestSealRecordsEncrypterDescription(t *testing.T) {
	sealed, err := Seal([]byte("body"), reversingEncrypter{})
	require.NoError(t, err)

	var env Envelope
	require.NoError(t, json.Unmarshal(sealed, &env))
	require.Equal(t, EnvelopeVersion, env.EnvelopeVersion)
	require.NotNil(t, env.Encrypter)
	require.Equal(t, "reversing", env.Encrypter.Name)
	require.Equal(t, map[string]any{"direction": "backward"}, env.Encrypter.Body)
}

func TestSealOmitsBodyForEmptyConfig(t *testing.T) {
	sealed, err := Seal([]byte("body"), failingEncrypter{})
	require.NoError(t, err)

	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(sealed, &raw))
	var ref map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw["encrypter"], &ref))
	assert.Contains(t, ref, "name")
	assert.NotContains(t, ref, "body",
		"a description without configuration should record only the name")
}

func TestOpenPassesNilForMissingRef(t *testing.T) {
	raw, err := json.Marshal(Envelope{
		EnvelopeVersion: EnvelopeVersion,
		Ciphertext:      reverseBytes([]byte("the body")),
	})
	require.NoError(t, err)

	body, err := Open(raw, func(got *Ref) (encrypt.Encrypter, error) {
		assert.Nil(t, got, "resolver should receive nil ref when none is in the envelope")
		return reversingEncrypter{}, nil
	})
	require.NoError(t, err)
	require.Equal(t, []byte("the body"), body)
}

func TestOpenRejectsUnknownEnvelopeVersion(t *testing.T) {
	raw, err := json.Marshal(Envelope{EnvelopeVersion: 99, Ciphertext: []byte("x")})
	require.NoError(t, err)

	_, err = Open(raw, func(*Ref) (encrypt.Encrypter, error) {
		return reversingEncrypter{}, nil
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "envelope-version 99")
}

func TestOpenReportsDecryptFailure(t *testing.T) {
	sealed, err := Seal([]byte("body"), failingEncrypter{})
	require.NoError(t, err)

	_, err = Open(sealed, func(*Ref) (encrypt.Encrypter, error) {
		return failingEncrypter{}, nil
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "decrypt")
}
