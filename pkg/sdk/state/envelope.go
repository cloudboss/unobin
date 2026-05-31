package state

import (
	"encoding/json"
	"fmt"

	"github.com/cloudboss/unobin/pkg/sdk/encrypt"
)

// EnvelopeVersion is the on-disk version of the envelope that wraps a plan
// or a state snapshot. Bump it when the envelope itself changes; the inner
// body keeps its own format version, which moves independently.
const EnvelopeVersion = 1

// Ref names one entry in a library's StateBackends or Encrypters map.
// Alias is empty for the bare names registered under the built-in core
// library (`local`, `env-key`); otherwise it is the import alias from
// `imports:`. Body holds the operator-provided configuration the
// resolver decodes against the backend or encrypter type's schema.
//
// A plan file records a Backend ref so apply can reconstruct the same
// backend without re-reading config.ub, and an Encrypter ref rides in the
// envelope so a reader can build the encrypter before opening the body.
type Ref struct {
	Alias string         `json:"alias,omitempty"`
	Name  string         `json:"name"`
	Body  map[string]any `json:"body,omitempty"`
}

// Envelope is the on-disk container for a plan or a state snapshot. The
// envelope is plaintext; it names which encrypter sealed the inner body so
// a reader can build the matching encrypter before opening it. The
// Encrypter ref holds only non-secret configuration (env-var name, KMS
// key id, region). Key material is never on disk; the operator must have it
// available through the encrypter's own channel.
type Envelope struct {
	EnvelopeVersion int    `json:"envelope-version"`
	Encrypter       *Ref   `json:"encrypter,omitempty"`
	Ciphertext      []byte `json:"ciphertext"`
}

// Seal encrypts body with enc and wraps the result in an Envelope ready for
// atomic write. encRef names the encrypter that produced the ciphertext;
// pass nil when the reader resolves the encrypter by other means (a state
// backend already holds its encrypter, so it needs no ref).
func Seal(body []byte, encRef *Ref, enc encrypt.Encrypter) ([]byte, error) {
	sealed, err := enc.Encrypt(body)
	if err != nil {
		return nil, fmt.Errorf("seal: encrypt: %w", err)
	}
	env := Envelope{
		EnvelopeVersion: EnvelopeVersion,
		Encrypter:       encRef,
		Ciphertext:      sealed,
	}
	out, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("envelope: %w", err)
	}
	return append(out, '\n'), nil
}

// Open reads an envelope and returns the decrypted inner body. resolveEnc
// builds an encrypter from the envelope's ref; it receives nil when the
// envelope omitted the ref.
func Open(b []byte, resolveEnc func(*Ref) (encrypt.Encrypter, error)) ([]byte, error) {
	var env Envelope
	if err := json.Unmarshal(b, &env); err != nil {
		return nil, fmt.Errorf("envelope: %w", err)
	}
	if env.EnvelopeVersion != EnvelopeVersion {
		return nil, fmt.Errorf(
			"envelope: unsupported envelope-version %d (this build expects %d)",
			env.EnvelopeVersion, EnvelopeVersion)
	}
	enc, err := resolveEnc(env.Encrypter)
	if err != nil {
		return nil, err
	}
	body, err := enc.Decrypt(env.Ciphertext)
	if err != nil {
		return nil, fmt.Errorf("open: decrypt: %w", err)
	}
	return body, nil
}
