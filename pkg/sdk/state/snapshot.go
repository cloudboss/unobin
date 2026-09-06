package state

// FactoryInfo identifies the stack a snapshot belongs to. ContentRevision
// is the content-addressable hash the binary was compiled with.
type FactoryInfo struct {
	Name            string `json:"name"`
	Version         string `json:"version"`
	ContentRevision string `json:"content-revision"`
}
