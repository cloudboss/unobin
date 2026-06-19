package encrypt

import "github.com/cloudboss/unobin/pkg/sdk/cfg"

// EncrypterType registers one of the fixed set of state encrypters.
// Configuration describes the schema for the `encryption:` block
// fields the operator writes (e.g., env-var for the env-key
// encrypter). New is the factory the runtime invokes once it has
// decoded the configuration against that schema; body holds the same
// fields undecoded, keyed by operator-facing name, for encrypters
// that include them in their Description.
type EncrypterType struct {
	Name          string
	Description   string
	Configuration cfg.Registration
	New           func(config any, body map[string]any) (Encrypter, error)
}
