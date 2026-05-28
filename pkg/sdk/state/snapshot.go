package state

import (
	"encoding/json"
	"fmt"
	"time"
)

// CurrentFormatVersion is the schema version this package reads and writes
// for snapshots. Older versions error on read.
const CurrentFormatVersion = 1

// EntryType discriminates the three records a snapshot can hold.
type EntryType string

const (
	EntryLeaf        EntryType = "leaf"
	EntryLibraryCall EntryType = "library-call"
	EntryAction      EntryType = "action"
)

// Entry is one record in a snapshot. Type discriminates the fields used:
// leaf entries hold a primitive resource's Kind, SchemaVersion, Inputs, and
// Outputs; library-call entries hold a composite type's Library, LibraryType,
// and call-site Inputs/Outputs.
//
// SensitiveInputs and SensitiveOutputs name the kebab-case fields whose
// values came from a sensitive source. Renderers mask the matching
// entries when printing.
type Entry struct {
	Address string    `json:"address"`
	Type    EntryType `json:"type"`

	Kind             string   `json:"kind,omitempty"`
	SchemaVersion    int      `json:"schema-version,omitempty"`
	SensitiveInputs  []string `json:"sensitive-inputs,omitempty"`
	SensitiveOutputs []string `json:"sensitive-outputs,omitempty"`

	Library     string `json:"library,omitempty"`
	LibraryType string `json:"library-type,omitempty"`

	// Configuration names the library configuration the resource was
	// created against, as "<alias>.<configuration>". It is recorded only
	// when that differs from the import's own default, since destroy and
	// refresh need it to find the right credentials once the resource is
	// no longer described in source.
	Configuration string `json:"configuration,omitempty"`

	TriggerHash string `json:"trigger-hash,omitempty"`

	Inputs    map[string]any `json:"inputs,omitempty"`
	Outputs   map[string]any `json:"outputs,omitempty"`
	DependsOn []string       `json:"depends-on,omitempty"`
}

// FactoryInfo identifies the stack a snapshot belongs to. ContentRevision
// is the content-addressable hash the binary was compiled with.
type FactoryInfo struct {
	Name            string `json:"name"`
	Version         string `json:"version"`
	ContentRevision string `json:"content-revision"`
}

// Snapshot is the in-memory record of one state file. The runtime reads
// the current snapshot at the start of plan or apply, and writes a fresh
// one after each successful resource action.
type Snapshot struct {
	FormatVersion int            `json:"format-version"`
	Factory       FactoryInfo    `json:"stack"`
	Stack         string         `json:"deployment-id"`
	GeneratedAt   time.Time      `json:"generated-at"`
	Entries       []*Entry       `json:"entries"`
	Outputs       map[string]any `json:"outputs,omitempty"`
}

// NewSnapshot returns an empty snapshot at the current schema version.
func NewSnapshot(stack FactoryInfo, deploymentID string) *Snapshot {
	return &Snapshot{
		FormatVersion: CurrentFormatVersion,
		Factory:       stack,
		Stack:         deploymentID,
		GeneratedAt:   time.Now().UTC(),
		Entries:       nil,
	}
}

// Find returns the entry at address, or nil.
func (s *Snapshot) Find(address string) *Entry {
	for _, e := range s.Entries {
		if e.Address == address {
			return e
		}
	}
	return nil
}

// EncodeSnapshot serializes s as pretty-printed JSON with a trailing
// newline. Map keys are sorted by encoding/json so two encodes of the same
// snapshot produce identical bytes.
func EncodeSnapshot(s *Snapshot) ([]byte, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("snapshot: %w", err)
	}
	return append(b, '\n'), nil
}

// DecodeSnapshot parses a snapshot from JSON bytes.
func DecodeSnapshot(b []byte) (*Snapshot, error) {
	var s Snapshot
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, fmt.Errorf("snapshot: %w", err)
	}
	if s.FormatVersion != CurrentFormatVersion {
		return nil, fmt.Errorf("snapshot: unsupported format-version %d (this build expects %d)",
			s.FormatVersion, CurrentFormatVersion)
	}
	if err := s.Validate(); err != nil {
		return nil, err
	}
	return &s, nil
}

// Validate checks every entry's discriminator and required fields, and
// rejects duplicate addresses within a snapshot.
func (s *Snapshot) Validate() error {
	if s.FormatVersion != CurrentFormatVersion {
		return fmt.Errorf("snapshot: format-version is %d, expected %d",
			s.FormatVersion, CurrentFormatVersion)
	}
	seen := make(map[string]bool, len(s.Entries))
	for i, e := range s.Entries {
		if e == nil {
			return fmt.Errorf("snapshot: entries[%d] is nil", i)
		}
		if e.Address == "" {
			return fmt.Errorf("snapshot: entries[%d] missing address", i)
		}
		if seen[e.Address] {
			return fmt.Errorf("snapshot: duplicate address %q", e.Address)
		}
		seen[e.Address] = true
		if err := e.validate(); err != nil {
			return err
		}
	}
	return nil
}

func (e *Entry) validate() error {
	switch e.Type {
	case EntryLeaf:
		if e.Kind == "" {
			return fmt.Errorf("snapshot: leaf entry %q missing kind", e.Address)
		}
	case EntryLibraryCall:
		if e.Library == "" {
			return fmt.Errorf("snapshot: library-call entry %q missing library", e.Address)
		}
		if e.LibraryType == "" {
			return fmt.Errorf("snapshot: library-call entry %q missing library-type", e.Address)
		}
	case EntryAction:
		if e.Kind == "" {
			return fmt.Errorf("snapshot: action entry %q missing kind", e.Address)
		}
	case "":
		return fmt.Errorf("snapshot: entry %q missing type", e.Address)
	default:
		return fmt.Errorf("snapshot: entry %q has unknown type %q", e.Address, e.Type)
	}
	return nil
}
