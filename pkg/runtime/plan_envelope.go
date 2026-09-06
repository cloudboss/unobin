package runtime

import (
	"github.com/cloudboss/unobin/pkg/sdk/encrypt"
	"github.com/cloudboss/unobin/pkg/sdk/state"
)

// SealPlanV2 encrypts a validated version 2 plan in the shared envelope.
func SealPlanV2(plan PlanFileV2, enc encrypt.Encrypter) ([]byte, error) {
	body, err := EncodePlanV2(plan)
	if err != nil {
		return nil, err
	}
	return state.Seal(body, state.PayloadTypePlan, enc)
}

// OpenPlanV2 decrypts an envelope and validates its version 2 plan body.
func OpenPlanV2(
	b []byte,
	resolveEnc func(*StateRef) (encrypt.Encrypter, error),
) (PlanFileV2, error) {
	body, err := state.Open(b, state.PayloadTypePlan, resolveEnc)
	if err != nil {
		return PlanFileV2{}, err
	}
	return DecodePlanV2(body)
}
