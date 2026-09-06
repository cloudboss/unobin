package runtime

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/cloudboss/unobin/pkg/diagnostic"
)

// TriggerAlways is the literal an action uses to opt into running every
// time, regardless of stored state.
const TriggerAlways = "always"

func hashJSON(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", diagnostic.Context("trigger hash", err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}
