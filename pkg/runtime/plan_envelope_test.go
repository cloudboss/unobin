package runtime

import (
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
func (reversingEncrypter) Describe() encrypt.Description {
	return encrypt.Description{
		KeySource: "reversing",
		Config:    map[string]any{"direction": "backward"},
	}
}

func reverse(b []byte) []byte {
	out := make([]byte, len(b))
	for i, x := range b {
		out[len(b)-1-i] = x
	}
	return out
}

func TestSealOpenPlanFileV2PreservesPlan(t *testing.T) {
	plan := validPlanFileV2(t)

	sealed, err := SealPlanV2(plan, reversingEncrypter{})
	require.NoError(t, err)

	opened, err := OpenPlanV2(
		sealed,
		func(ref *StateRef) (encrypt.Encrypter, error) {
			require.NotNil(t, ref)
			assert.Equal(t, "reversing", ref.Name)
			assert.Equal(t, "backward", ref.Body["direction"])
			return reversingEncrypter{}, nil
		},
	)
	require.NoError(t, err)
	assert.Equal(t, plan, opened)
}

func TestSealPlanFileV2RejectsInvalidPlan(t *testing.T) {
	plan := validPlanFileV2(t)
	plan.Stack = ""

	sealed, err := SealPlanV2(plan, reversingEncrypter{})
	require.ErrorContains(t, err, "stack is required")
	assert.Nil(t, sealed)
}

func TestOpenPlanFileV2RejectsObsoletePlan(t *testing.T) {
	sealed, err := state.Seal(
		[]byte(`{"format-version":1}`),
		state.PayloadTypePlan,
		reversingEncrypter{},
	)
	require.NoError(t, err)

	plan, err := OpenPlanV2(
		sealed,
		func(*StateRef) (encrypt.Encrypter, error) {
			return reversingEncrypter{}, nil
		},
	)
	require.ErrorContains(t, err, "obsolete alpha format")
	assert.Equal(t, PlanFileV2{}, plan)
}
