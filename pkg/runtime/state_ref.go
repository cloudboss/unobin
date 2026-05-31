package runtime

import "github.com/cloudboss/unobin/pkg/sdk/state"

// StateRef is an alias for state.Ref, the resolver reference that both the
// plan envelope and the state-snapshot envelope use to name a backend or
// encrypter. The type lives in pkg/sdk/state so a state backend can use it
// without importing runtime; the alias keeps the runtime spelling for the
// plan body and the resolver code.
type StateRef = state.Ref
