package runtime

import (
	"github.com/cloudboss/unobin/pkg/sdk/encrypt"
	"github.com/cloudboss/unobin/pkg/sdk/state"
)

// SealPlan encodes p and seals the body in the shared state.Envelope,
// ready for atomic write to disk. encRef names the encrypter that produced
// the ciphertext; pass nil when the operator omitted the encryption block
// and the resolver chose a default (env-key against UB_STATE_KEY, or the
// no-op when that env var is unset).
func SealPlan(p *Plan, encRef *StateRef, enc encrypt.Encrypter) ([]byte, error) {
	body, err := EncodePlan(p)
	if err != nil {
		return nil, err
	}
	return state.Seal(body, encRef, enc)
}

// OpenPlan opens a sealed plan envelope and returns its inner PlanFile.
// resolveEnc builds an encrypter from the envelope's ref; pass a function
// that consults the caller's resolver chain. resolveEnc receives nil when
// the envelope omitted the encrypter (operator used the default chain at
// plan time).
func OpenPlan(
	b []byte,
	resolveEnc func(*StateRef) (encrypt.Encrypter, error),
) (*PlanFile, error) {
	body, err := state.Open(b, resolveEnc)
	if err != nil {
		return nil, err
	}
	return DecodePlan(body)
}
