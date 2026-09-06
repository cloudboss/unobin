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
	Address          string                     `json:"address"`
	Type             sdkstate.StateEntryKind    `json:"entry-kind"`
	Category         string                     `json:"category,omitempty"`
	Binding          *sdkstate.CanonicalBinding `json:"binding,omitempty"`
	SchemaVersion    int                        `json:"schema-version,omitempty"`
	SensitiveInputs  []string                   `json:"sensitive-inputs,omitempty"`
	SensitiveOutputs []string                   `json:"sensitive-outputs,omitempty"`
	Inputs           map[string]any             `json:"inputs,omitempty"`
	Outputs          map[string]any             `json:"outputs,omitempty"`
	DependsOn        []string                   `json:"depends-on,omitempty"`
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
	revision, err := store.CurrentRev()
	var snap *sdkstate.SnapshotV2
	if err == nil {
		snap, err = store.GetV2(revision)
	}
	if errors.Is(err, sdkstate.ErrNoCurrent) {
		snap, err = sdkstate.NewSnapshotV2(sdkstate.FactoryInfo{
			Name: c.Name, Version: "v0.0.0", ContentRevision: "empty",
		}, stackName)
		if err != nil {
			return "", err
		}
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

func summarizeSnapshot(snap *sdkstate.SnapshotV2) stateSummary {
	factory := snap.Factory
	factory.ContentRevision = "<revision>"
	entries := make([]stateEntrySummary, 0, len(snap.Entries))
	for _, entry := range snap.Entries {
		entries = append(entries, summarizeEntry(&entry))
	}
	slices.SortFunc(entries, func(a, b stateEntrySummary) int {
		return strings.Compare(a.Address, b.Address)
	})
	return stateSummary{
		FormatVersion: snap.FormatVersion,
		Factory:       factory,
		Stack:         snap.Stack,
		Entries:       entries,
		Outputs:       summaryObject(snap.Outputs),
	}
}

func summarizeEntry(entry *sdkstate.StateEntryV2) stateEntrySummary {
	summary := stateEntrySummary{Address: entry.Address, Type: entry.Kind,
		Category: string(entry.Kind)}
	switch entry.Kind {
	case sdkstate.StateResource:
		target := entry.Payload.Resource.Target
		summary.Binding, summary.SchemaVersion = &target.Binding, target.SchemaVersion
		summary.Inputs, summary.Outputs = summaryObject(target.Inputs), summaryObject(target.Outputs)
		summary.DependsOn = sortedCopy(target.DependsOn)
		summary.SensitiveInputs = sortedCopy(target.SensitiveInputPaths)
		summary.SensitiveOutputs = sortedCopy(target.SensitiveOutputPaths)
	case sdkstate.StateAction:
		action := entry.Payload.Action
		summary.Binding = &action.Binding
		summary.Inputs, summary.Outputs = summaryObject(action.Inputs), summaryObject(action.Outputs)
		summary.DependsOn = sortedCopy(action.DependsOn)
		summary.SensitiveInputs = sortedCopy(action.SensitiveInputPaths)
		summary.SensitiveOutputs = sortedCopy(action.SensitiveOutputPaths)
	case sdkstate.StateDataSource:
		data := entry.Payload.DataSource
		summary.Binding = &data.Binding
		summary.Inputs, summary.Outputs = summaryObject(data.Inputs), summaryObject(data.Outputs)
		summary.DependsOn = sortedCopy(data.DependsOn)
		summary.SensitiveInputs = sortedCopy(data.SensitiveInputPaths)
		summary.SensitiveOutputs = sortedCopy(data.SensitiveOutputPaths)
	case sdkstate.StateComposite:
		composite := entry.Payload.Composite
		summary.Category, summary.Binding = composite.Category, &composite.Binding
		summary.Inputs = summaryObject(composite.Inputs)
		summary.Outputs = summaryObject(composite.Outputs)
		summary.DependsOn = sortedCopy(composite.DependsOn)
		summary.SensitiveInputs = sortedCopy(composite.SensitiveInputPaths)
		summary.SensitiveOutputs = sortedCopy(composite.SensitiveOutputPaths)
	}
	return summary
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
