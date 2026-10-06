package state

import (
	"encoding/json"
	"fmt"
	"slices"
	"time"
)

// CurrentFormatVersion is the schema version this package reads and writes
// for snapshots. Older versions error on read.
const CurrentFormatVersion = 2

// Binding identifies the implementation selected for an entry.
type Binding struct {
	Alias       string `json:"alias"`
	LibraryPath string `json:"library-path,omitempty"`
	Export      string `json:"kind,omitempty"`
}

// Entry describes a resource, data observation, action, or composite call.
// Category identifies the address namespace. Composite marks a call boundary
// independently of its category. Binding identifies its implementation.
//
// SensitiveInputs and SensitiveOutputs name the kebab-case fields whose
// values came from a sensitive source. Renderers mask the matching
// entries when printing.
type Entry struct {
	Address   string `json:"address"`
	Category  string `json:"category"`
	Composite bool   `json:"composite"`

	Binding          *Binding `json:"binding,omitempty"`
	SchemaVersion    int      `json:"schema-version,omitempty"`
	SensitiveInputs  []string `json:"sensitive-inputs,omitempty"`
	SensitiveOutputs []string `json:"sensitive-outputs,omitempty"`

	TriggerHash string `json:"trigger-hash,omitempty"`

	Inputs  map[string]any `json:"inputs,omitempty"`
	Outputs map[string]any `json:"outputs,omitempty"`

	// Configuration contains the library settings reviewed for this resource.
	// The current configuration supplies access settings; this value identifies
	// the target that was managed when the entry was written.
	Configuration map[string]any `json:"configuration,omitempty"`
	DependsOn     []string       `json:"depends-on,omitempty"`
}

type entryJSON struct {
	Address          string         `json:"address"`
	Category         string         `json:"category"`
	Composite        bool           `json:"composite"`
	Binding          *Binding       `json:"binding,omitempty"`
	SchemaVersion    int            `json:"schema-version,omitempty"`
	SensitiveInputs  []string       `json:"sensitive-inputs,omitempty"`
	SensitiveOutputs []string       `json:"sensitive-outputs,omitempty"`
	TriggerHash      string         `json:"trigger-hash,omitempty"`
	Inputs           map[string]any `json:"inputs,omitempty"`
	Outputs          map[string]any `json:"outputs,omitempty"`
	Configuration    map[string]any `json:"configuration,omitempty"`
	DependsOn        []string       `json:"depends-on,omitempty"`
}

func (e *Entry) MarshalJSON() ([]byte, error) {
	return json.Marshal(entryJSON{
		Address:          e.Address,
		Category:         e.Category,
		Composite:        e.Composite,
		Binding:          e.Binding,
		SchemaVersion:    e.SchemaVersion,
		SensitiveInputs:  e.SensitiveInputs,
		SensitiveOutputs: e.SensitiveOutputs,
		TriggerHash:      e.TriggerHash,
		Inputs:           e.Inputs,
		Outputs:          e.Outputs,
		Configuration:    e.Configuration,
		DependsOn:        e.DependsOn,
	})
}

func (e *Entry) UnmarshalJSON(b []byte) error {
	var raw struct {
		entryJSON
		Composite *bool `json:"composite"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	if raw.Composite == nil {
		return fmt.Errorf("entry %q missing composite", raw.Address)
	}
	raw.entryJSON.Composite = *raw.Composite
	*e = Entry(raw.entryJSON)
	return nil
}

// FactoryInfo identifies the stack a snapshot belongs to. ContentRevision
// is the content-addressable hash the binary was compiled with.
type FactoryInfo struct {
	Name            string `json:"name"`
	Version         string `json:"version"`
	ContentRevision string `json:"content-revision"`
}

// Snapshot is the in-memory contents of one state file. The runtime reads
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
	var header struct {
		FormatVersion int `json:"format-version"`
	}
	if err := json.Unmarshal(b, &header); err != nil {
		return nil, fmt.Errorf("snapshot: %w", err)
	}
	if header.FormatVersion != CurrentFormatVersion {
		return nil, fmt.Errorf(
			"snapshot: unsupported format-version %d (this build expects %d); recreate the state",
			header.FormatVersion,
			CurrentFormatVersion,
		)
	}
	var s Snapshot
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, fmt.Errorf("snapshot: %w", err)
	}
	if err := s.Validate(); err != nil {
		return nil, err
	}
	return &s, nil
}

// Validate checks every entry's category and required fields, and
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
	if err := e.validateCategory("resource", "data-source", "action"); err != nil {
		return err
	}
	return e.validateGraphBinding()
}

func (e *Entry) validateCategory(allowed ...string) error {
	if e.Category == "" {
		return fmt.Errorf("snapshot: entry %q missing category", e.Address)
	}
	if slices.Contains(allowed, e.Category) {
		return nil
	}
	return fmt.Errorf("snapshot: entry %q has category %q", e.Address, e.Category)
}

func (e *Entry) validateGraphBinding() error {
	if e.Binding == nil {
		return fmt.Errorf("snapshot: entry %q binding missing", e.Address)
	}
	if e.Binding.Alias == "" {
		return fmt.Errorf("snapshot: entry %q binding missing alias", e.Address)
	}
	if e.Binding.Export == "" {
		return fmt.Errorf("snapshot: entry %q binding missing kind", e.Address)
	}
	return nil
}
