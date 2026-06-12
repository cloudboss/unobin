package encrypt

// Encrypter seals and unseals opaque bytes. Implementations cover one
// key source each: an env var holding a 32-byte symmetric key, a KMS
// service that wraps a per-snapshot data key, and so on.
type Encrypter interface {
	Encrypt(plaintext []byte) ([]byte, error)
	Decrypt(ciphertext []byte) ([]byte, error)

	// Describe reports which key source this encrypter is and the
	// non-secret configuration a reader needs to decrypt what it
	// sealed. Envelope sealing calls Describe after Encrypt, so the
	// result may include facts resolved while encrypting, such as
	// the kms encrypter's key ARN.
	Describe() Description
}

// Description identifies a key source and the configuration that
// builds the same encrypter again. KeySource is the registry name an
// operator selects with @key-source. Config holds the configuration
// by operator-facing field name and must stay decodable against the
// key source's configuration schema. Key material never belongs in a
// Description: descriptions are written to disk in plaintext.
type Description struct {
	KeySource string
	Config    map[string]any
}

// Well-known Description config keys. Each must match the name the
// config decoder derives from the key source's schema struct.
const (
	ConfigKeyID  = "key-id"
	ConfigEnvVar = "env-var"
)
