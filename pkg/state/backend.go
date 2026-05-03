package state

import "context"

// Backend is the abstraction over snapshot storage. The runtime reads
// and writes snapshots through it; concrete implementations decide
// where the bytes live. `LocalStore` is the on-disk implementation;
// cloud-backed implementations (S3, GCS, Azure-blob) plug in here.
//
// Apply and refresh acquire the deployment's lock through Lock and
// release it through the returned `Lock`. Plan is read-only and never
// locks. ForceUnlock is the escape hatch for a leaked lock.
type Backend interface {
	DeploymentID() string
	Current() (*Snapshot, error)
	CurrentRev() (string, error)
	Get(rev string) (*Snapshot, error)
	Write(snap *Snapshot) (string, error)
	SetCurrent(rev string) error
	List() ([]string, error)
	Lock(ctx context.Context) (Lock, error)
	ForceUnlock() error
}

// Lock is a held exclusion on one deployment. Callers must invoke
// Unlock; a leaked lock blocks future apply and refresh runs until an
// operator calls ForceUnlock.
type Lock interface {
	Unlock() error
}

var _ Backend = (*LocalStore)(nil)
