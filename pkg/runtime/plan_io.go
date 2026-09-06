package runtime

// FactoryRef identifies the stack a plan was computed against.
type FactoryRef struct {
	Name            string `json:"name"`
	Version         string `json:"version"`
	ContentRevision string `json:"content-revision"`
}
