package runtime

import (
	"github.com/cloudboss/unobin/pkg/sdk/encrypt"
	"github.com/cloudboss/unobin/pkg/sdk/state"
)

// SealPlan encodes p and seals the body in the shared state.Envelope,
// ready for atomic write. The envelope records the encrypter's own
// description, whether the operator wrote an encryption block or the
// resolver chose a default.
func SealPlan(p *Plan, enc encrypt.Encrypter) ([]byte, error) {
	body, err := EncodePlan(p)
	if err != nil {
		return nil, err
	}
	return state.Seal(body, enc)
}

// OpenPlan opens a sealed plan envelope and returns its inner PlanFile.
// resolveEnc builds an encrypter from the envelope's ref; pass a function
// that consults the caller's resolver chain. resolveEnc receives nil when
// the envelope has no encrypter ref. A plan file may name its own
// encrypter because the decrypted content is exactly what apply executes,
// so the ref grants the file no authority its content does not already
// have.
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
