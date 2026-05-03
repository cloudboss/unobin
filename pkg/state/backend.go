package state

// Backend is the abstraction over snapshot storage. The runtime reads
// and writes snapshots through it; concrete implementations decide
// where the bytes live. `LocalStore` is the on-disk implementation;
// cloud-backed implementations (S3, GCS, Azure-blob) plug in here.
type Backend interface {
	DeploymentID() string
	Current() (*Snapshot, error)
	CurrentRev() (string, error)
	Get(rev string) (*Snapshot, error)
	Write(snap *Snapshot) (string, error)
	SetCurrent(rev string) error
	List() ([]string, error)
}

var _ Backend = (*LocalStore)(nil)
