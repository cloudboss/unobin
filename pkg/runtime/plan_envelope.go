package runtime

import (
	"encoding/json"
	"fmt"

	"github.com/cloudboss/unobin/pkg/sdk/encrypt"
)

// EnvelopeVersion is the on-disk version of the plan-file envelope.
// Bump when the envelope itself changes; the inner plan body has its
// own PlanFormatVersion that moves independently.
const EnvelopeVersion = 1

// PlanEnvelope is the on-disk container for a plan file. The
// envelope is plaintext; it identifies which encrypter sealed the
// inner body so apply can construct the matching encrypter before
// opening the body. The Encrypter ref carries only non-secret
// configuration (env-var name, KMS key id, region). Key material
// itself is never on disk; the operator must have it available at
// apply time through whatever channel the encrypter uses
// (UB_STATE_KEY env var, cloud SDK credentials, etc.).
type PlanEnvelope struct {
	EnvelopeVersion int       `json:"envelope-version"`
	Encrypter       *StateRef `json:"encrypter,omitempty"`
	Ciphertext      []byte    `json:"ciphertext"`
}

// SealPlan encodes p, encrypts the body with enc, and wraps the
// result in a PlanEnvelope ready for atomic write to disk. encRef
// names the encrypter that produced the ciphertext; pass nil when
// the operator omitted the encryption block and the resolver chose
// a default (env-key against UB_STATE_KEY, or the no-op when that
// env var is unset).
func SealPlan(p *Plan, encRef *StateRef, enc encrypt.Encrypter) ([]byte, error) {
	body, err := EncodePlan(p)
	if err != nil {
		return nil, err
	}
	sealed, err := enc.Encrypt(body)
	if err != nil {
		return nil, fmt.Errorf("plan: encrypt: %w", err)
	}
	env := PlanEnvelope{
		EnvelopeVersion: EnvelopeVersion,
		Encrypter:       encRef,
		Ciphertext:      sealed,
	}
	out, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("plan envelope: %w", err)
	}
	return append(out, '\n'), nil
}

// OpenPlan reads a plan-file envelope and returns its inner
// PlanFile. resolveEnc constructs an encrypter from the envelope's
// Encrypter ref; pass a function that consults the caller's
// resolver chain. resolveEnc receives nil when the envelope omitted
// the encrypter (operator used the default chain at plan time).
func OpenPlan(
	b []byte,
	resolveEnc func(*StateRef) (encrypt.Encrypter, error),
) (*PlanFile, error) {
	var env PlanEnvelope
	if err := json.Unmarshal(b, &env); err != nil {
		return nil, fmt.Errorf("plan envelope: %w", err)
	}
	if env.EnvelopeVersion != EnvelopeVersion {
		return nil, fmt.Errorf(
			"plan envelope: unsupported envelope-version %d (this build expects %d)",
			env.EnvelopeVersion, EnvelopeVersion)
	}
	enc, err := resolveEnc(env.Encrypter)
	if err != nil {
		return nil, err
	}
	body, err := enc.Decrypt(env.Ciphertext)
	if err != nil {
		return nil, fmt.Errorf("plan: decrypt: %w", err)
	}
	return DecodePlan(body)
}
