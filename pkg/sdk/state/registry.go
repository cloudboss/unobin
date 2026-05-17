package state

import (
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
	"github.com/cloudboss/unobin/pkg/sdk/encrypt"
)

// BackendType registers a state backend a provider module ships.
// Configuration describes the schema for the `state:` block fields
// the operator writes (e.g., path for the local backend, bucket and
// region for an S3 backend). New is the factory the runtime invokes
// once it has decoded the configuration against that schema; it
// returns a ready-to-use Backend.
type BackendType struct {
	Name          string
	Description   string
	Configuration *cfg.ConfigurationType
	New           func(config any, stack, deploymentID string, enc encrypt.Encrypter) (Backend, error)
}
