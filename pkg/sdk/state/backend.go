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
	Current() (*Snapshot, error)
	CurrentRev() (string, error)
	Get(rev string) (*Snapshot, error)
	Write(snap *Snapshot) (string, error)
	SetCurrent(rev string) error
	List() ([]string, error)
	Delete(rev string) error
	Lock(ctx context.Context) (Lock, error)
	ForceUnlock() error
}

// Lock is a held exclusion on one stack. Callers must invoke
// Unlock; a leaked lock blocks future apply and refresh runs until an
// operator calls ForceUnlock.
type Lock interface {
	Unlock() error
}

// ErrNoCurrent is returned by Backend.Current and Backend.CurrentRev when
// no snapshot has been written for the stack yet.
var ErrNoCurrent = errors.New("no current snapshot")
