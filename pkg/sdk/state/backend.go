package state

import (
	"context"
	"errors"
)

// Backend is the contract a state backend satisfies. The runtime reads
// and writes snapshots through it; concrete implementations decide
// where the bytes live. Apply and refresh acquire the stack's
// lock through Lock and release it through the returned Lock value.
// Plan is read-only and never locks. ForceUnlock is the escape hatch
// for a leaked lock.
type Backend interface {
	Stack() string
	CurrentRev() (string, error)
	SetCurrent(rev string) error
	// List returns snapshot revisions from oldest to newest.
	List() ([]string, error)
	Delete(rev string) error
	Lock(ctx context.Context) (Lock, error)
	ForceUnlock() error
}

// SnapshotBackendV2 reads and writes strict version-2 snapshots by revision.
// CurrentRev and SetCurrent remain on Backend so snapshot persistence can keep
// the existing write-before-current-pointer contract.
type SnapshotBackendV2 interface {
	GetV2(rev string) (*SnapshotV2, error)
	WriteV2(snap *SnapshotV2) (string, error)
}

// Lock is a held exclusion on one stack. Callers must invoke
// Unlock; a leaked lock blocks future apply and refresh runs until an
// operator calls ForceUnlock.
type Lock interface {
	Unlock() error
}

// ErrNoCurrent is returned by Backend.CurrentRev when
// no snapshot has been written for the stack yet.
var ErrNoCurrent = errors.New("no current snapshot")
