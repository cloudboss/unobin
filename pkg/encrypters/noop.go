package encrypters

// Noop passes bytes through unchanged. Useful in tests and for dev
// workflows where the operator has explicitly opted out of encryption.
type Noop struct{}

func (Noop) Encrypt(p []byte) ([]byte, error) { return p, nil }
func (Noop) Decrypt(p []byte) ([]byte, error) { return p, nil }
