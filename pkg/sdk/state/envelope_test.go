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

// failingEncrypter stands in for a wrong key: its Decrypt always
// errors. The configurable description exercises how decrypt errors
// report what sealed the envelope.
type failingEncrypter struct {
	desc encrypt.Description
}

func (failingEncrypter) Encrypt(b []byte) ([]byte, error) { return b, nil }
func (failingEncrypter) Decrypt([]byte) ([]byte, error) {
	return nil, errors.New("authentication failed")
}
func (e failingEncrypter) Describe() encrypt.Description { return e.desc }

func reverseBytes(b []byte) []byte {
	out := make([]byte, len(b))
	for i, x := range b {
		out[len(b)-1-i] = x
	}
	return out
}

func TestSealOpenRoundTrip(t *testing.T) {
	sealed, err := Seal([]byte("the body"), PayloadTypeState, reversingEncrypter{})
	require.NoError(t, err)

	body, err := Open(sealed, PayloadTypeState, func(got *Ref) (encrypt.Encrypter, error) {
		require.NotNil(t, got, "envelope should include the encrypter ref")
		assert.Equal(t, "reversing", got.Name)
		assert.Equal(t, map[string]any{"direction": "backward"}, got.Body)
		return reversingEncrypter{}, nil
	})
	require.NoError(t, err)
	require.Equal(t, []byte("the body"), body)
}

func TestSealRecordsEncrypterDescription(t *testing.T) {
	sealed, err := Seal([]byte("body"), PayloadTypeState, reversingEncrypter{})
	require.NoError(t, err)

	var env Envelope
	require.NoError(t, json.Unmarshal(sealed, &env))
	require.Equal(t, EnvelopeVersion, env.EnvelopeVersion)
	require.Equal(t, PayloadTypeState, env.PayloadType)
	require.NotNil(t, env.Encrypter)
	require.Equal(t, "reversing", env.Encrypter.Name)
	require.Equal(t, map[string]any{"direction": "backward"}, env.Encrypter.Body)
}

func TestSealOmitsBodyForEmptyConfig(t *testing.T) {
	sealed, err := Seal([]byte("body"), PayloadTypeState, failingEncrypter{
		desc: encrypt.Description{KeySource: "failing"},
	})
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

	body, err := Open(raw, PayloadTypeState, func(got *Ref) (encrypt.Encrypter, error) {
		assert.Nil(t, got, "resolver should receive nil ref when none is in the envelope")
		return reversingEncrypter{}, nil
	})
	require.NoError(t, err)
	require.Equal(t, []byte("the body"), body)
}

func TestOpenRejectsUnknownEnvelopeVersion(t *testing.T) {
	raw, err := json.Marshal(Envelope{EnvelopeVersion: 99, Ciphertext: []byte("x")})
	require.NoError(t, err)

	_, err = Open(raw, PayloadTypeState, func(*Ref) (encrypt.Encrypter, error) {
		return reversingEncrypter{}, nil
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "envelope-version 99")
}

func TestOpenRejectsMismatchedPayloadType(t *testing.T) {
	raw, err := json.Marshal(Envelope{
		EnvelopeVersion: EnvelopeVersion,
		PayloadType:     PayloadTypePlan,
		Ciphertext:      reverseBytes([]byte("the body")),
	})
	require.NoError(t, err)

	called := false
	_, err = Open(raw, PayloadTypeState, func(*Ref) (encrypt.Encrypter, error) {
		called = true
		return reversingEncrypter{}, nil
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "payload-type plan, expected state")
	assert.False(t, called)
}

func TestOpenDecryptFailureNamesKeySource(t *testing.T) {
	tests := []struct {
		name string
		desc encrypt.Description
		hint string
	}{
		{
			name: "key-id",
			desc: encrypt.Description{
				KeySource: "kms",
				Config: map[string]any{
					"key-id": "arn:aws:kms:us-east-1:000000000000:key/abc",
					"aws":    map[string]any{"region": "us-east-1"},
				},
			},
			hint: "sealed with kms key-id arn:aws:kms:us-east-1:000000000000:key/abc",
		},
		{
			name: "env-var",
			desc: encrypt.Description{
				KeySource: "env-key",
				Config:    map[string]any{"env-var": "UB_STATE_KEY"},
			},
			hint: "sealed with env-key env-var UB_STATE_KEY",
		},
		{
			name: "name only",
			desc: encrypt.Description{KeySource: "failing"},
			hint: "sealed with failing",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			enc := failingEncrypter{desc: tt.desc}
			sealed, err := Seal([]byte("body"), PayloadTypeState, enc)
			require.NoError(t, err)

			_, err = Open(sealed, PayloadTypeState, func(*Ref) (encrypt.Encrypter, error) {
				return enc, nil
			})
			require.Error(t, err)
			require.Contains(t, err.Error(), "decrypt")
			require.Contains(t, err.Error(), tt.hint)
		})
	}
}

func TestOpenDecryptFailureWithoutRefStaysPlain(t *testing.T) {
	raw, err := json.Marshal(Envelope{
		EnvelopeVersion: EnvelopeVersion,
		Ciphertext:      []byte("x"),
	})
	require.NoError(t, err)

	_, err = Open(raw, PayloadTypeState, func(*Ref) (encrypt.Encrypter, error) {
		return failingEncrypter{}, nil
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "decrypt")
	require.NotContains(t, err.Error(), "sealed with")
}
