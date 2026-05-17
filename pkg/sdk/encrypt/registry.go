package encrypt

import "github.com/cloudboss/unobin/pkg/sdk/cfg"

// EncrypterType registers an encrypter a provider module ships.
// Configuration describes the schema for the `encryption:` block
// fields the operator writes (e.g., env-var for the env-key
// encrypter). New is the factory the runtime invokes once it has
// decoded the configuration against that schema.
type EncrypterType struct {
	Name          string
	Description   string
	Configuration *cfg.ConfigurationType
	New           func(config any) (Encrypter, error)
}
