package state

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	internalconfig "github.com/cloudboss/unobin/internal/configuration"
	encodedvalue "github.com/cloudboss/unobin/pkg/encoding/value"
	"github.com/cloudboss/unobin/pkg/stateref"
)

const SnapshotFormatVersionV2 = 2

type CanonicalBinding struct {
	LibraryPath string `json:"library-path"`
	Export      string `json:"export"`
}

func (b CanonicalBinding) Validate() error {
	if b.LibraryPath == "" {
		return fmt.Errorf("library path is required")
	}
	if strings.TrimSpace(b.LibraryPath) != b.LibraryPath {
		return fmt.Errorf("library path is invalid")
	}
	if b.Export == "" {
		return fmt.Errorf("export is required")
	}
	if !validPersistedName(b.Export) {
		return fmt.Errorf("export is invalid: %q", b.Export)
	}
	return nil
}

type ResourceTarget struct {
	Binding              CanonicalBinding    `json:"binding"`
	SchemaVersion        int                 `json:"schema-version"`
	Inputs               encodedvalue.Value  `json:"inputs"`
	Outputs              encodedvalue.Value  `json:"outputs"`
	Configuration        ConfigurationRecord `json:"configuration"`
	Identity             IdentityRecord      `json:"identity"`
	DependsOn            []string            `json:"depends-on"`
	SensitiveInputPaths  []string            `json:"sensitive-input-paths"`
	SensitiveOutputPaths []string            `json:"sensitive-output-paths"`
}

func (t ResourceTarget) Validate() error {
	if err := t.Binding.Validate(); err != nil {
		return fmt.Errorf("binding: %w", err)
	}
	if t.SchemaVersion < 1 {
		return fmt.Errorf("schema version must be greater than zero")
	}
	if err := validateStateObject(t.Inputs, "inputs"); err != nil {
		return err
	}
	if err := validateStateObject(t.Outputs, "outputs"); err != nil {
		return err
	}
	if err := t.Configuration.Validate(); err != nil {
		return fmt.Errorf("configuration: %w", err)
	}
	if t.Configuration.LibraryPath != t.Binding.LibraryPath {
		return fmt.Errorf("configuration library path does not match binding")
	}
	if err := t.Identity.Validate(); err != nil {
		return fmt.Errorf("identity: %w", err)
	}
	if err := validateRequiredStrings(t.DependsOn, "dependencies"); err != nil {
		return err
	}
	if err := validateDependencies(t.DependsOn); err != nil {
		return err
	}
	if t.SensitiveInputPaths == nil {
		return fmt.Errorf("sensitive input paths are required")
	}
	if err := internalconfig.ValidatePaths(
		t.Inputs,
		t.SensitiveInputPaths,
		"sensitive input",
	); err != nil {
		return err
	}
	if t.SensitiveOutputPaths == nil {
		return fmt.Errorf("sensitive output paths are required")
	}
	if err := internalconfig.ValidatePaths(
		t.Outputs,
		t.SensitiveOutputPaths,
		"sensitive output",
	); err != nil {
		return err
	}
	return nil
}

type ResourceStatePayload struct {
	Target ResourceTarget `json:"target"`
}

func (p ResourceStatePayload) Validate() error {
	return p.Target.Validate()
}

type ActionStatePayload struct {
	Binding              CanonicalBinding    `json:"binding"`
	Inputs               encodedvalue.Value  `json:"inputs"`
	Outputs              encodedvalue.Value  `json:"outputs"`
	Configuration        ConfigurationRecord `json:"configuration"`
	TriggerHash          string              `json:"trigger-hash"`
	DependsOn            []string            `json:"depends-on"`
	SensitiveInputPaths  []string            `json:"sensitive-input-paths"`
	SensitiveOutputPaths []string            `json:"sensitive-output-paths"`
}

func (p ActionStatePayload) Validate() error {
	return validateProviderStatePayload(
		p.Binding,
		p.Inputs,
		p.Outputs,
		p.Configuration,
		p.DependsOn,
		p.SensitiveInputPaths,
		p.SensitiveOutputPaths,
	)
}

type DataSourceStatePayload struct {
	Binding              CanonicalBinding    `json:"binding"`
	Inputs               encodedvalue.Value  `json:"inputs"`
	Outputs              encodedvalue.Value  `json:"outputs"`
	Configuration        ConfigurationRecord `json:"configuration"`
	DependsOn            []string            `json:"depends-on"`
	SensitiveInputPaths  []string            `json:"sensitive-input-paths"`
	SensitiveOutputPaths []string            `json:"sensitive-output-paths"`
}

func (p DataSourceStatePayload) Validate() error {
	return validateProviderStatePayload(
		p.Binding,
		p.Inputs,
		p.Outputs,
		p.Configuration,
		p.DependsOn,
		p.SensitiveInputPaths,
		p.SensitiveOutputPaths,
	)
}

type CompositeStatePayload struct {
	Category             string             `json:"category"`
	Binding              CanonicalBinding   `json:"binding"`
	Inputs               encodedvalue.Value `json:"inputs"`
	Outputs              encodedvalue.Value `json:"outputs"`
	DependsOn            []string           `json:"depends-on"`
	SensitiveInputPaths  []string           `json:"sensitive-input-paths"`
	SensitiveOutputPaths []string           `json:"sensitive-output-paths"`
}

func (p CompositeStatePayload) Validate() error {
	if !validStateCategory(p.Category) {
		return fmt.Errorf("category is invalid: %q", p.Category)
	}
	if err := p.Binding.Validate(); err != nil {
		return fmt.Errorf("binding: %w", err)
	}
	if err := validateStateObject(p.Inputs, "inputs"); err != nil {
		return err
	}
	if err := validateStateObject(p.Outputs, "outputs"); err != nil {
		return err
	}
	return validateStateMetadata(
		p.Inputs,
		p.Outputs,
		p.DependsOn,
		p.SensitiveInputPaths,
		p.SensitiveOutputPaths,
	)
}

type StateEntryKind string

const (
	StateResource   StateEntryKind = "resource"
	StateAction     StateEntryKind = "action"
	StateDataSource StateEntryKind = "data-source"
	StateComposite  StateEntryKind = "composite"
)

type StatePayload struct {
	Kind       StateEntryKind          `json:"kind"`
	Resource   *ResourceStatePayload   `json:"resource,omitempty"`
	Action     *ActionStatePayload     `json:"action,omitempty"`
	DataSource *DataSourceStatePayload `json:"data-source,omitempty"`
	Composite  *CompositeStatePayload  `json:"composite,omitempty"`
}

func (p StatePayload) Validate(kind StateEntryKind) error {
	if p.Kind != kind {
		return fmt.Errorf("payload kind %s does not match entry kind %s", p.Kind, kind)
	}
	switch kind {
	case StateResource:
		if p.Resource == nil {
			return fmt.Errorf("resource payload is required")
		}
		if p.Action != nil || p.DataSource != nil || p.Composite != nil {
			return fmt.Errorf("resource payload forbids other payloads")
		}
		return p.Resource.Validate()
	case StateAction:
		if p.Action == nil {
			return fmt.Errorf("action payload is required")
		}
		if p.Resource != nil || p.DataSource != nil || p.Composite != nil {
			return fmt.Errorf("action payload forbids other payloads")
		}
		return p.Action.Validate()
	case StateDataSource:
		if p.DataSource == nil {
			return fmt.Errorf("data-source payload is required")
		}
		if p.Resource != nil || p.Action != nil || p.Composite != nil {
			return fmt.Errorf("data-source payload forbids other payloads")
		}
		return p.DataSource.Validate()
	case StateComposite:
		if p.Composite == nil {
			return fmt.Errorf("composite payload is required")
		}
		if p.Resource != nil || p.Action != nil || p.DataSource != nil {
			return fmt.Errorf("composite payload forbids other payloads")
		}
		return p.Composite.Validate()
	default:
		return fmt.Errorf("unknown state entry kind %q", kind)
	}
}

type StateEntryV2 struct {
	Address string         `json:"address"`
	Kind    StateEntryKind `json:"kind"`
	Payload StatePayload   `json:"payload"`
}

func (e StateEntryV2) Validate() error {
	if err := e.Payload.Validate(e.Kind); err != nil {
		return err
	}
	category, err := stateAddressCategory(e.Address)
	if err != nil {
		return fmt.Errorf("address is invalid: %w", err)
	}
	expectedCategory := string(e.Kind)
	if e.Kind == StateComposite {
		expectedCategory = e.Payload.Composite.Category
	}
	if category != expectedCategory {
		return fmt.Errorf(
			"address category %s does not match %s",
			category,
			expectedCategory,
		)
	}
	return nil
}

type SnapshotV2 struct {
	FormatVersion  int                `json:"format-version"`
	Factory        FactoryInfo        `json:"factory"`
	Stack          string             `json:"stack"`
	GeneratedAt    time.Time          `json:"generated-at"`
	Entries        []StateEntryV2     `json:"entries"`
	Outputs        encodedvalue.Value `json:"outputs"`
	SensitivePaths []string           `json:"sensitive-paths"`
}

func NewSnapshotV2(factory FactoryInfo, stack string) (*SnapshotV2, error) {
	outputs, err := encodedvalue.Object(map[string]encodedvalue.Value{})
	if err != nil {
		return nil, fmt.Errorf("initialize snapshot outputs: %w", err)
	}
	snapshot := &SnapshotV2{
		FormatVersion:  SnapshotFormatVersionV2,
		Factory:        factory,
		Stack:          stack,
		GeneratedAt:    time.Now().UTC(),
		Entries:        []StateEntryV2{},
		Outputs:        outputs,
		SensitivePaths: []string{},
	}
	if err := snapshot.Validate(); err != nil {
		return nil, fmt.Errorf("snapshot: %w", err)
	}
	return snapshot, nil
}

func (s *SnapshotV2) Clone() (*SnapshotV2, error) {
	if s == nil {
		return nil, fmt.Errorf("snapshot is required")
	}
	encoded, err := EncodeSnapshotV2(*s)
	if err != nil {
		return nil, fmt.Errorf("copy snapshot: %w", err)
	}
	cloned, err := DecodeSnapshotV2(encoded)
	if err != nil {
		return nil, fmt.Errorf("copy snapshot: %w", err)
	}
	return &cloned, nil
}

func (s *SnapshotV2) Find(address string) *StateEntryV2 {
	if s == nil {
		return nil
	}
	for i := range s.Entries {
		if s.Entries[i].Address == address {
			return &s.Entries[i]
		}
	}
	return nil
}

func (s *SnapshotV2) SetOutputs(
	outputs encodedvalue.Value,
	sensitivePaths []string,
) error {
	if s == nil {
		return fmt.Errorf("snapshot is required")
	}
	if err := s.Validate(); err != nil {
		return fmt.Errorf("snapshot: %w", err)
	}

	next := *s
	next.Outputs = outputs
	next.SensitivePaths = slices.Clone(sensitivePaths)
	if err := next.Validate(); err != nil {
		return fmt.Errorf("snapshot: %w", err)
	}
	*s = next
	return nil
}

func (s *SnapshotV2) SetEntry(entry StateEntryV2) error {
	if s == nil {
		return fmt.Errorf("snapshot is required")
	}
	if err := s.Validate(); err != nil {
		return fmt.Errorf("snapshot: %w", err)
	}
	if err := entry.Validate(); err != nil {
		return fmt.Errorf("entry: %w", err)
	}

	next := *s
	next.Entries = slices.Clone(s.Entries)
	index, found := slices.BinarySearchFunc(
		next.Entries,
		entry.Address,
		func(candidate StateEntryV2, address string) int {
			return strings.Compare(candidate.Address, address)
		},
	)
	entry, err := cloneStateEntryV2(entry)
	if err != nil {
		return err
	}
	if found {
		next.Entries[index] = entry
	} else {
		next.Entries = slices.Insert(next.Entries, index, entry)
	}
	if err := next.Validate(); err != nil {
		return fmt.Errorf("snapshot: %w", err)
	}
	*s = next
	return nil
}

func (s *SnapshotV2) RemoveEntry(address string) error {
	if s == nil {
		return fmt.Errorf("snapshot is required")
	}
	if err := s.Validate(); err != nil {
		return fmt.Errorf("snapshot: %w", err)
	}
	if _, err := stateAddressCategory(address); err != nil {
		return fmt.Errorf("entry address is invalid: %w", err)
	}
	index, found := slices.BinarySearchFunc(
		s.Entries,
		address,
		func(candidate StateEntryV2, address string) int {
			return strings.Compare(candidate.Address, address)
		},
	)
	if !found {
		return nil
	}

	next := *s
	next.Entries = slices.Delete(slices.Clone(s.Entries), index, index+1)
	if err := next.Validate(); err != nil {
		return fmt.Errorf("snapshot: %w", err)
	}
	*s = next
	return nil
}

func cloneStateEntryV2(entry StateEntryV2) (StateEntryV2, error) {
	encoded, err := json.Marshal(entry)
	if err != nil {
		return StateEntryV2{}, fmt.Errorf("copy entry: %w", err)
	}
	var result StateEntryV2
	if err := json.Unmarshal(encoded, &result); err != nil {
		return StateEntryV2{}, fmt.Errorf("copy entry: %w", err)
	}
	return result, nil
}

func (s SnapshotV2) Validate() error {
	if s.FormatVersion != SnapshotFormatVersionV2 {
		return fmt.Errorf("format version must be %d", SnapshotFormatVersionV2)
	}
	if err := validateFactoryInfo(s.Factory); err != nil {
		return err
	}
	if s.Stack == "" {
		return fmt.Errorf("stack is required")
	}
	if s.GeneratedAt.IsZero() {
		return fmt.Errorf("generated time is required")
	}
	_, offset := s.GeneratedAt.Zone()
	if offset != 0 {
		return fmt.Errorf("generated time must use UTC")
	}
	if s.Entries == nil {
		return fmt.Errorf("entries are required")
	}
	for i := range s.Entries {
		if i > 0 && s.Entries[i].Address <= s.Entries[i-1].Address {
			return fmt.Errorf("entries must have unique addresses sorted by UTF-8 bytes")
		}
		if err := s.Entries[i].Validate(); err != nil {
			return fmt.Errorf("entries[%d]: %w", i, err)
		}
	}
	if err := validateStateObject(s.Outputs, "outputs"); err != nil {
		return err
	}
	if s.SensitivePaths == nil {
		return fmt.Errorf("sensitive paths are required")
	}
	return internalconfig.ValidatePaths(s.Outputs, s.SensitivePaths, "sensitive")
}

func validPersistedName(value string) bool {
	if value == "" || !persistedLetter(value[0]) {
		return false
	}
	for i := 1; i < len(value); i++ {
		char := value[i]
		if persistedLetter(char) || char >= '0' && char <= '9' || char == '-' {
			continue
		}
		return false
	}
	last := value[len(value)-1]
	return persistedLetter(last) || last >= '0' && last <= '9'
}

func persistedLetter(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z'
}

func validateStateObject(value encodedvalue.Value, subject string) error {
	if _, err := json.Marshal(value); err != nil {
		return fmt.Errorf("%s are invalid: %w", subject, err)
	}
	if value.HasPending() {
		return fmt.Errorf("%s must be concrete", subject)
	}
	if _, ok := value.ObjectFields(); !ok {
		return fmt.Errorf("%s must be an object", subject)
	}
	return nil
}

func validateProviderStatePayload(
	binding CanonicalBinding,
	inputs encodedvalue.Value,
	outputs encodedvalue.Value,
	configuration ConfigurationRecord,
	dependsOn []string,
	sensitiveInputPaths []string,
	sensitiveOutputPaths []string,
) error {
	if err := binding.Validate(); err != nil {
		return fmt.Errorf("binding: %w", err)
	}
	if err := validateStateObject(inputs, "inputs"); err != nil {
		return err
	}
	if err := validateStateObject(outputs, "outputs"); err != nil {
		return err
	}
	if err := configuration.Validate(); err != nil {
		return fmt.Errorf("configuration: %w", err)
	}
	if configuration.LibraryPath != binding.LibraryPath {
		return fmt.Errorf("configuration library path does not match binding")
	}
	return validateStateMetadata(
		inputs,
		outputs,
		dependsOn,
		sensitiveInputPaths,
		sensitiveOutputPaths,
	)
}

func validateStateMetadata(
	inputs encodedvalue.Value,
	outputs encodedvalue.Value,
	dependsOn []string,
	sensitiveInputPaths []string,
	sensitiveOutputPaths []string,
) error {
	if err := validateRequiredStrings(dependsOn, "dependencies"); err != nil {
		return err
	}
	if err := validateDependencies(dependsOn); err != nil {
		return err
	}
	if sensitiveInputPaths == nil {
		return fmt.Errorf("sensitive input paths are required")
	}
	if err := internalconfig.ValidatePaths(
		inputs,
		sensitiveInputPaths,
		"sensitive input",
	); err != nil {
		return err
	}
	if sensitiveOutputPaths == nil {
		return fmt.Errorf("sensitive output paths are required")
	}
	return internalconfig.ValidatePaths(
		outputs,
		sensitiveOutputPaths,
		"sensitive output",
	)
}

func validateRequiredStrings(values []string, subject string) error {
	if values == nil {
		return fmt.Errorf("%s are required", subject)
	}
	for i, value := range values {
		if value == "" {
			return fmt.Errorf("%s contain an empty value", subject)
		}
		if i > 0 && value <= values[i-1] {
			return fmt.Errorf("%s must be unique and sorted", subject)
		}
	}
	return nil
}

func validateDependencies(dependencies []string) error {
	for _, dependency := range dependencies {
		if err := stateref.ValidateAddress(dependency); err != nil {
			return fmt.Errorf("dependency is invalid: %q", dependency)
		}
	}
	return nil
}

func validStateCategory(category string) bool {
	switch category {
	case "resource", "action", "data-source":
		return true
	default:
		return false
	}
}

func stateAddressCategory(address string) (string, error) {
	ref, err := stateref.ParseStateRef(address)
	if err != nil {
		return "", err
	}
	return string(ref.Segments[len(ref.Segments)-1].Category), nil
}

func validateFactoryInfo(factory FactoryInfo) error {
	if factory.Name == "" {
		return fmt.Errorf("factory name is required")
	}
	if factory.Version == "" {
		return fmt.Errorf("factory version is required")
	}
	if factory.ContentRevision == "" {
		return fmt.Errorf("factory content revision is required")
	}
	return nil
}
