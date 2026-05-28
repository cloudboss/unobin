package runtime

// StateRef names one entry in a library's StateBackends or Encrypters
// map. Alias is empty for the bare names registered under the
// built-in core library (`local`, `env-key`); otherwise it is the
// import alias from `imports:`. Body carries the operator-provided
// configuration the resolver decodes against the backend or
// encrypter type's schema.
//
// The plan file carries a Backend StateRef so apply can reconstruct
// the same backend the plan was computed against without re-reading
// config.ub. An Encrypter StateRef rides in the plan envelope, not
// the plan body, since the encrypter is needed to open the body
// itself.
type StateRef struct {
	Alias string         `json:"alias,omitempty"`
	Name  string         `json:"name"`
	Body  map[string]any `json:"body,omitempty"`
}
