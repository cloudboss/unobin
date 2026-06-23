package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudboss/unobin/pkg/sdk/encrypt"
)

// EnvelopeVersion is the on-disk version of the envelope that wraps a plan
// or a state snapshot. Bump it when the envelope itself changes; the inner
// body keeps its own format version, which moves independently.
const EnvelopeVersion = 1

// PayloadType labels the plaintext body sealed inside an Envelope.
type PayloadType string

const (
	// PayloadTypePlan marks a sealed plan file body.
	PayloadTypePlan PayloadType = "plan"
	// PayloadTypeState marks a sealed state snapshot body.
	PayloadTypeState PayloadType = "state"
)

// Ref names an entry in the fixed backend or encrypter registry. Name is
// the bare state backend or encryption key-source from the stack file;
// Body is the configuration the resolver decodes against that entry's schema.
//
// A plan file records a Backend ref so apply can reconstruct the same
// backend without re-reading the stack file, and every envelope records an
// Encrypter ref naming the key source that sealed it.
type Ref struct {
	Name string         `json:"name"`
	Body map[string]any `json:"body,omitempty"`
}

// Envelope is the on-disk container for a plan or a state snapshot. The
// envelope is plaintext; its Encrypter ref records which key source sealed
// the inner body and the non-secret configuration (env-var name, KMS key
// ARN, region) needed to decrypt it, so a state file found long after the
// config that wrote it still says how to get back to plaintext. Key
// material is never on disk; the operator must have it available through
// the encrypter's own channel.
//
// The envelope is not authenticated, so a reader with its own
// configuration must not let the file choose the key: state backends
// decrypt with the encrypter resolved from the stack file and treat the
// recorded ref as information for operators and error messages.
type Envelope struct {
	EnvelopeVersion int         `json:"envelope-version"`
	PayloadType     PayloadType `json:"payload-type,omitempty"`
	Encrypter       *Ref        `json:"encrypter,omitempty"`
	Ciphertext      []byte      `json:"ciphertext"`
}

// Seal encrypts body with enc and wraps the result in an Envelope ready for
// atomic write. The envelope records enc's description, taken after
// encrypting so it includes facts resolved on first use, like the kms
// encrypter's key ARN.
func Seal(body []byte, payloadType PayloadType, enc encrypt.Encrypter) ([]byte, error) {
	if payloadType == "" {
		return nil, errors.New("envelope: payload-type is required")
	}
	sealed, err := enc.Encrypt(body)
	if err != nil {
		return nil, fmt.Errorf("seal: encrypt: %w", err)
	}
	d := enc.Describe()
	env := Envelope{
		EnvelopeVersion: EnvelopeVersion,
		PayloadType:     payloadType,
		Encrypter:       &Ref{Name: d.KeySource, Body: d.Config},
		Ciphertext:      sealed,
	}
	out, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("envelope: %w", err)
	}
	return append(out, '\n'), nil
}

// Open reads an envelope and returns the decrypted inner body. resolveEnc
// builds or selects an encrypter from the envelope's ref; it receives nil
// when the envelope has no encrypter ref.
func Open(
	b []byte,
	expectedPayloadType PayloadType,
	resolveEnc func(*Ref) (encrypt.Encrypter, error),
) ([]byte, error) {
	var env Envelope
	if err := json.Unmarshal(b, &env); err != nil {
		return nil, fmt.Errorf("envelope: %w", err)
	}
	if env.EnvelopeVersion != EnvelopeVersion {
		return nil, fmt.Errorf(
			"envelope: unsupported envelope-version %d (this build expects %d)",
			env.EnvelopeVersion, EnvelopeVersion)
	}
	if expectedPayloadType != "" && env.PayloadType != "" && env.PayloadType != expectedPayloadType {
		return nil, fmt.Errorf(
			"envelope: payload-type %s, expected %s", env.PayloadType, expectedPayloadType)
	}
	enc, err := resolveEnc(env.Encrypter)
	if err != nil {
		return nil, err
	}
	body, err := enc.Decrypt(env.Ciphertext)
	if err != nil {
		return nil, fmt.Errorf("open: decrypt: %w%s", err, refHint(env.Encrypter))
	}
	return body, nil
}

// refHint names the key source that sealed the envelope and, when
// recorded, the key that can open it, for decrypt errors.
func refHint(ref *Ref) string {
	if ref == nil {
		return ""
	}
	var hint strings.Builder
	hint.WriteString(" (sealed with " + ref.Name)
	for _, key := range []string{encrypt.ConfigKeyID, encrypt.ConfigEnvVar} {
		if v, ok := ref.Body[key].(string); ok && v != "" {
			hint.WriteString(" " + key + " " + v)
			break
		}
	}
	return hint.String() + ")"
}
