package state

import (
	"encoding/json"
	"fmt"
	"slices"
	"time"
)

// CurrentFormatVersion is the schema version this package reads and writes
// for snapshots. Older versions error on read.
const CurrentFormatVersion = 1

// EntryType discriminates the records a snapshot can hold.
type EntryType string

const (
	EntryLeaf        EntryType = "leaf"
	EntryLibraryCall EntryType = "library-call"
	EntryAction      EntryType = "action"
	// EntryData records what a data source read during the last
	// apply. Nothing in the world belongs to it, so removing the node
	// removes the record without a destroy.
	EntryData EntryType = "data-source"
)

// Selector identifies the implementation selected for an entry.
type Selector struct {
	Alias  string `json:"alias"`
	Export string `json:"export,omitempty"`
}

// Entry is one record in a snapshot. Type is the entry discriminator.
// Kind is the graph node kind for resource, data-source, action, and
// composite entries. Selector names the implementation used by that entry.
//
// SensitiveInputs and SensitiveOutputs name the kebab-case fields whose
// values came from a sensitive source. Renderers mask the matching
// entries when printing.
type Entry struct {
	Address string    `json:"address"`
	Type    EntryType `json:"entry-kind"`

	Kind             string    `json:"node-kind,omitempty"`
	Selector         *Selector `json:"selector,omitempty"`
	SchemaVersion    int       `json:"schema-version,omitempty"`
	SensitiveInputs  []string  `json:"sensitive-inputs,omitempty"`
	SensitiveOutputs []string  `json:"sensitive-outputs,omitempty"`

	TriggerHash string `json:"trigger-hash,omitempty"`

	Inputs    map[string]any `json:"inputs,omitempty"`
	Outputs   map[string]any `json:"outputs,omitempty"`
	DependsOn []string       `json:"depends-on,omitempty"`
}

type entryJSON struct {
	Address          string         `json:"address"`
	Type             EntryType      `json:"entry-kind"`
	Kind             string         `json:"node-kind,omitempty"`
	Selector         *Selector      `json:"selector,omitempty"`
	SchemaVersion    int            `json:"schema-version,omitempty"`
	SensitiveInputs  []string       `json:"sensitive-inputs,omitempty"`
	SensitiveOutputs []string       `json:"sensitive-outputs,omitempty"`
	TriggerHash      string         `json:"trigger-hash,omitempty"`
	Inputs           map[string]any `json:"inputs,omitempty"`
	Outputs          map[string]any `json:"outputs,omitempty"`
	DependsOn        []string       `json:"depends-on,omitempty"`
}

func (e *Entry) MarshalJSON() ([]byte, error) {
	return json.Marshal(entryJSON{
		Address:          e.Address,
		Type:             e.Type,
		Kind:             e.Kind,
		Selector:         e.Selector,
		SchemaVersion:    e.SchemaVersion,
		SensitiveInputs:  e.SensitiveInputs,
		SensitiveOutputs: e.SensitiveOutputs,
		TriggerHash:      e.TriggerHash,
		Inputs:           e.Inputs,
		Outputs:          e.Outputs,
		DependsOn:        e.DependsOn,
	})
}

func (e *Entry) UnmarshalJSON(b []byte) error {
	var raw entryJSON
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	*e = Entry(raw)
	return nil
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
	Factory       FactoryInfo    `json:"factory"`
	Stack         string         `json:"stack"`
	GeneratedAt   time.Time      `json:"generated-at"`
	Entries       []*Entry       `json:"entries"`
	Outputs       map[string]any `json:"outputs,omitempty"`
}

// NewSnapshot returns an empty snapshot at the current schema version.
func NewSnapshot(factory FactoryInfo, stack string) *Snapshot {
	return &Snapshot{
		FormatVersion: CurrentFormatVersion,
		Factory:       factory,
		Stack:         stack,
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
		if err := e.validateNodeKind("resource"); err != nil {
			return err
		}
		if err := e.validateGraphSelector(); err != nil {
			return err
		}
	case EntryLibraryCall:
		if err := e.validateNodeKind("resource", "data-source", "action"); err != nil {
			return err
		}
		if err := e.validateGraphSelector(); err != nil {
			return err
		}
	case EntryAction:
		if err := e.validateNodeKind("action"); err != nil {
			return err
		}
		if err := e.validateGraphSelector(); err != nil {
			return err
		}
	case EntryData:
		if err := e.validateNodeKind("data-source"); err != nil {
			return err
		}
		if err := e.validateGraphSelector(); err != nil {
			return err
		}
	case "":
		return fmt.Errorf("snapshot: entry %q missing entry-kind", e.Address)
	default:
		return fmt.Errorf("snapshot: entry %q has unknown entry-kind %q", e.Address, e.Type)
	}
	return nil
}

func (e *Entry) validateNodeKind(allowed ...string) error {
	if e.Kind == "" {
		return fmt.Errorf("snapshot: entry %q missing node-kind", e.Address)
	}
	if slices.Contains(allowed, e.Kind) {
		return nil
	}
	return fmt.Errorf("snapshot: entry %q has node-kind %q", e.Address, e.Kind)
}

func (e *Entry) validateGraphSelector() error {
	if e.Selector == nil {
		return fmt.Errorf("snapshot: entry %q selector missing", e.Address)
	}
	if e.Selector.Alias == "" {
		return fmt.Errorf("snapshot: entry %q selector missing alias", e.Address)
	}
	if e.Selector.Export == "" {
		return fmt.Errorf("snapshot: entry %q selector missing export", e.Address)
	}
	return nil
}
