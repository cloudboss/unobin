package encrypters

import sdkencrypt "github.com/cloudboss/unobin/pkg/sdk/encrypt"

// Noop passes bytes through unchanged. Useful in tests and for dev
// workflows where the operator has explicitly opted out of encryption.
type Noop struct{}

func (Noop) Encrypt(p []byte) ([]byte, error) { return p, nil }
func (Noop) Decrypt(p []byte) ([]byte, error) { return p, nil }

// Describe names the noop key source. The empty Config marks the
// sealed body as plaintext with nothing more to configure.
func (Noop) Describe() sdkencrypt.Description {
	return sdkencrypt.Description{KeySource: NoopName}
}
