package e2etest

import (
	"bytes"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"strings"

	"github.com/cloudboss/unobin/pkg/encrypters"
	sdkstate "github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/cloudboss/unobin/pkg/state/local"
)

type stateSummary struct {
	FormatVersion int                  `json:"format-version"`
	Factory       sdkstate.FactoryInfo `json:"factory"`
	Stack         string               `json:"stack"`
	Entries       []stateEntrySummary  `json:"entries"`
	Outputs       map[string]any       `json:"outputs,omitempty"`
}

type stateEntrySummary struct {
	Address          string             `json:"address"`
	Type             sdkstate.EntryType `json:"entry-kind"`
	Kind             string             `json:"node-kind,omitempty"`
	Binding          *sdkstate.Binding  `json:"binding,omitempty"`
	SchemaVersion    int                `json:"schema-version,omitempty"`
	SensitiveInputs  []string           `json:"sensitive-inputs,omitempty"`
	SensitiveOutputs []string           `json:"sensitive-outputs,omitempty"`
	Inputs           map[string]any     `json:"inputs,omitempty"`
	Outputs          map[string]any     `json:"outputs,omitempty"`
	DependsOn        []string           `json:"depends-on,omitempty"`
}

func compareStateSummary(
	caseDir string,
	workspace string,
	c CompiledCase,
	stackPath string,
	doUpdate bool,
) error {
	body, err := stateSummaryJSON(workspace, c, stackPath)
	if err != nil {
		return err
	}
	return compareOptionalGolden(caseDir, "state summary", c.StateSummary, body, doUpdate)
}

func stateSummaryJSON(workspace string, c CompiledCase, stackPath string) (string, error) {
	stackName := stackNameFromPath(stackPath)
	store, err := local.NewStore(
		filepath.Join(workspace, ".unobin", "state"),
		c.Name,
		stackName,
		encrypters.Noop{},
	)
	if err != nil {
		return "", err
	}
	snap, err := store.Current()
	if errors.Is(err, sdkstate.ErrNoCurrent) {
		snap = sdkstate.NewSnapshot(sdkstate.FactoryInfo{Name: c.Name}, stackName)
	} else if err != nil {
		return "", err
	}
	summary := summarizeSnapshot(snap)
	var body bytes.Buffer
	enc := json.NewEncoder(&body)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(summary); err != nil {
		return "", err
	}
	return body.String(), nil
}

func summarizeSnapshot(snap *sdkstate.Snapshot) stateSummary {
	factory := snap.Factory
	factory.ContentRevision = "<revision>"
	entries := make([]stateEntrySummary, 0, len(snap.Entries))
	for _, entry := range snap.Entries {
		entries = append(entries, summarizeEntry(entry))
	}
	slices.SortFunc(entries, func(a, b stateEntrySummary) int {
		return strings.Compare(a.Address, b.Address)
	})
	return stateSummary{
		FormatVersion: snap.FormatVersion,
		Factory:       factory,
		Stack:         snap.Stack,
		Entries:       entries,
		Outputs:       snap.Outputs,
	}
}

func summarizeEntry(entry *sdkstate.Entry) stateEntrySummary {
	return stateEntrySummary{
		Address:          entry.Address,
		Type:             entry.Type,
		Kind:             entry.Kind,
		Binding:          entry.Binding,
		SchemaVersion:    entry.SchemaVersion,
		SensitiveInputs:  sortedCopy(entry.SensitiveInputs),
		SensitiveOutputs: sortedCopy(entry.SensitiveOutputs),
		Inputs:           entry.Inputs,
		Outputs:          entry.Outputs,
		DependsOn:        sortedCopy(entry.DependsOn),
	}
}

func sortedCopy(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := slices.Clone(in)
	slices.Sort(out)
	return out
}

func stackNameFromPath(path string) string {
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	return strings.TrimSuffix(base, ext)
}
