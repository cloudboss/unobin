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

// failingEncrypter stands in for a wrong key: its Decrypt always errors.
type failingEncrypter struct{}

func (failingEncrypter) Encrypt(b []byte) ([]byte, error) { return b, nil }
func (failingEncrypter) Decrypt([]byte) ([]byte, error) {
	return nil, errors.New("authentication failed")
}

func reverseBytes(b []byte) []byte {
	out := make([]byte, len(b))
	for i, x := range b {
		out[len(b)-1-i] = x
	}
	return out
}

func TestSealOpenRoundTrip(t *testing.T) {
	ref := &Ref{Name: "kms", Body: map[string]any{"key-id": "alias/p"}}
	sealed, err := Seal([]byte("the body"), ref, reversingEncrypter{})
	require.NoError(t, err)

	body, err := Open(sealed, func(got *Ref) (encrypt.Encrypter, error) {
		require.NotNil(t, got, "envelope should include the encrypter ref")
		assert.Equal(t, "kms", got.Name)
		assert.Equal(t, "alias/p", got.Body["key-id"])
		return reversingEncrypter{}, nil
	})
	require.NoError(t, err)
	require.Equal(t, []byte("the body"), body)
}

func TestSealOmitsEncrypterRefWhenNil(t *testing.T) {
	sealed, err := Seal([]byte("body"), nil, reversingEncrypter{})
	require.NoError(t, err)

	var env Envelope
	require.NoError(t, json.Unmarshal(sealed, &env))
	require.Nil(t, env.Encrypter)
	require.Equal(t, EnvelopeVersion, env.EnvelopeVersion)

	_, err = Open(sealed, func(got *Ref) (encrypt.Encrypter, error) {
		assert.Nil(t, got, "resolver should receive nil ref when none is in the envelope")
		return reversingEncrypter{}, nil
	})
	require.NoError(t, err)
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
	sealed, err := Seal([]byte("body"), nil, failingEncrypter{})
	require.NoError(t, err)

	_, err = Open(sealed, func(*Ref) (encrypt.Encrypter, error) {
		return failingEncrypter{}, nil
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "decrypt")
}
