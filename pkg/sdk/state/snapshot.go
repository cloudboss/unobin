package state

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
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
	EntryData EntryType = "data"
)

// Selector identifies the implementation selected for an entry.
type Selector struct {
	Alias  string `json:"alias"`
	Export string `json:"export,omitempty"`
}

// ConfigurationRef identifies a named or default library configuration.
type ConfigurationRef struct {
	Kind     string   `json:"kind"`
	Name     string   `json:"name,omitempty"`
	Selector Selector `json:"selector"`
}

func EncodeConfigurationRef(ref string) (*ConfigurationRef, error) {
	if ref == "" {
		return nil, nil
	}
	parts := strings.Split(ref, ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, fmt.Errorf("configuration ref %q must be alias.name", ref)
	}
	out := &ConfigurationRef{Selector: Selector{Alias: parts[0]}}
	if parts[1] == "default" {
		out.Kind = "default"
		return out, nil
	}
	out.Kind = "named"
	out.Name = parts[1]
	return out, nil
}

func DecodeConfigurationRef(ref *ConfigurationRef) (string, error) {
	if ref == nil {
		return "", nil
	}
	if ref.Selector.Alias == "" {
		return "", fmt.Errorf("configuration selector missing alias")
	}
	if ref.Selector.Export != "" {
		return "", fmt.Errorf("configuration selector must have only alias")
	}
	switch ref.Kind {
	case "default":
		if ref.Name != "" {
			return "", fmt.Errorf("default configuration must not have name")
		}
		return ref.Selector.Alias + ".default", nil
	case "named":
		if ref.Name == "" {
			return "", fmt.Errorf("named configuration missing name")
		}
		return ref.Selector.Alias + "." + ref.Name, nil
	case "":
		return "", fmt.Errorf("configuration missing kind")
	default:
		return "", fmt.Errorf("unknown configuration kind %q", ref.Kind)
	}
}

func DecodeConfigurationRefJSON(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return "", nil
	}
	if bytes.HasPrefix(bytes.TrimSpace(raw), []byte("\"")) {
		return "", fmt.Errorf("configuration must be an object")
	}
	var ref ConfigurationRef
	if err := json.Unmarshal(raw, &ref); err != nil {
		return "", err
	}
	return DecodeConfigurationRef(&ref)
}

// Entry is one record in a snapshot. Type is the entry discriminator.
// Kind is the graph node kind for resource, data, action, and composite
// entries. Selector names the implementation used by that entry.
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

	// Configuration names the library configuration the resource was
	// created against, as "<alias>.<configuration>". It is recorded only
	// when that differs from the import's own default, since destroy and
	// refresh need it to find the right credentials once the resource is
	// no longer described in source.
	Configuration string `json:"-"`

	TriggerHash string `json:"trigger-hash,omitempty"`

	Inputs    map[string]any `json:"inputs,omitempty"`
	Outputs   map[string]any `json:"outputs,omitempty"`
	DependsOn []string       `json:"depends-on,omitempty"`
}

type entryJSON struct {
	Address          string            `json:"address"`
	Type             EntryType         `json:"entry-kind"`
	Kind             string            `json:"node-kind,omitempty"`
	Selector         *Selector         `json:"selector,omitempty"`
	SchemaVersion    int               `json:"schema-version,omitempty"`
	SensitiveInputs  []string          `json:"sensitive-inputs,omitempty"`
	SensitiveOutputs []string          `json:"sensitive-outputs,omitempty"`
	Configuration    *ConfigurationRef `json:"configuration,omitempty"`
	TriggerHash      string            `json:"trigger-hash,omitempty"`
	Inputs           map[string]any    `json:"inputs,omitempty"`
	Outputs          map[string]any    `json:"outputs,omitempty"`
	DependsOn        []string          `json:"depends-on,omitempty"`
}

func (e *Entry) MarshalJSON() ([]byte, error) {
	cfg, err := EncodeConfigurationRef(e.Configuration)
	if err != nil {
		return nil, err
	}
	return json.Marshal(entryJSON{
		Address:          e.Address,
		Type:             e.Type,
		Kind:             e.Kind,
		Selector:         e.Selector,
		SchemaVersion:    e.SchemaVersion,
		SensitiveInputs:  e.SensitiveInputs,
		SensitiveOutputs: e.SensitiveOutputs,
		Configuration:    cfg,
		TriggerHash:      e.TriggerHash,
		Inputs:           e.Inputs,
		Outputs:          e.Outputs,
		DependsOn:        e.DependsOn,
	})
}

func (e *Entry) UnmarshalJSON(b []byte) error {
	var raw struct {
		entryJSON
		Configuration json.RawMessage `json:"configuration"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	cfg, err := DecodeConfigurationRefJSON(raw.Configuration)
	if err != nil {
		return err
	}
	*e = Entry{
		Address:          raw.Address,
		Type:             raw.Type,
		Kind:             raw.Kind,
		Selector:         raw.Selector,
		SchemaVersion:    raw.SchemaVersion,
		SensitiveInputs:  raw.SensitiveInputs,
		SensitiveOutputs: raw.SensitiveOutputs,
		Configuration:    cfg,
		TriggerHash:      raw.TriggerHash,
		Inputs:           raw.Inputs,
		Outputs:          raw.Outputs,
		DependsOn:        raw.DependsOn,
	}
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
		if err := e.validateNodeKind("resource", "data", "action"); err != nil {
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
		if err := e.validateNodeKind("data"); err != nil {
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
