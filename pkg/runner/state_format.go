package runner

import (
	"fmt"
	"slices"

	"github.com/cloudboss/unobin/pkg/diagnostic"
	"github.com/cloudboss/unobin/pkg/filechange"
	"github.com/cloudboss/unobin/pkg/graphprint"
	"github.com/cloudboss/unobin/pkg/sdk/state"
)

type stateEntrySummary struct {
	Address   string             `json:"address"    ub:"address"`
	EntryType string             `json:"entry-type" ub:"entry-type"`
	Category  string             `json:"category"   ub:"category"`
	Binding   graphprint.Binding `json:"binding"    ub:"binding"`
}

type stateListResult struct {
	Kind          string                  `json:"kind"           ub:"kind"`
	FormatVersion int                     `json:"format-version" ub:"format-version"`
	Factory       factoryIdentity         `json:"factory"        ub:"factory"`
	Stack         string                  `json:"stack"          ub:"stack"`
	StateRev      *string                 `json:"state-rev"      ub:"state-rev"`
	Entries       []stateEntrySummary     `json:"entries"        ub:"entries"`
	Diagnostics   []diagnostic.Diagnostic `json:"diagnostics"    ub:"diagnostics"`
}

type stateEntryDetail struct {
	Address          string             `json:"address"           ub:"address"`
	EntryType        string             `json:"entry-type"        ub:"entry-type"`
	Category         string             `json:"category"          ub:"category"`
	Binding          graphprint.Binding `json:"binding"           ub:"binding"`
	SchemaVersion    int                `json:"schema-version"     ub:"schema-version"`
	TriggerHash      *string            `json:"trigger-hash"       ub:"trigger-hash"`
	Inputs           map[string]any     `json:"inputs"             ub:"inputs"`
	Outputs          map[string]any     `json:"outputs"            ub:"outputs"`
	DependsOn        []string           `json:"depends-on"         ub:"depends-on"`
	SensitiveInputs  []string           `json:"sensitive-inputs"   ub:"sensitive-inputs"`
	SensitiveOutputs []string           `json:"sensitive-outputs"  ub:"sensitive-outputs"`
}

type stateEntryResult struct {
	Kind          string                  `json:"kind"           ub:"kind"`
	FormatVersion int                     `json:"format-version" ub:"format-version"`
	Factory       factoryIdentity         `json:"factory"        ub:"factory"`
	Stack         string                  `json:"stack"          ub:"stack"`
	StateRev      string                  `json:"state-rev"      ub:"state-rev"`
	Entry         stateEntryDetail        `json:"entry"          ub:"entry"`
	Diagnostics   []diagnostic.Diagnostic `json:"diagnostics"    ub:"diagnostics"`
}

type stateSnapshotSummary struct {
	Revision string `json:"revision" ub:"revision"`
	Current  bool   `json:"current"  ub:"current"`
}

type stateSnapshotsResult struct {
	Kind          string                  `json:"kind"           ub:"kind"`
	FormatVersion int                     `json:"format-version" ub:"format-version"`
	Factory       factoryIdentity         `json:"factory"        ub:"factory"`
	Stack         string                  `json:"stack"          ub:"stack"`
	Current       *string                 `json:"current"        ub:"current"`
	Snapshots     []stateSnapshotSummary  `json:"snapshots"      ub:"snapshots"`
	Diagnostics   []diagnostic.Diagnostic `json:"diagnostics"    ub:"diagnostics"`
}

type pinResult struct {
	Kind          string                  `json:"kind"           ub:"kind"`
	FormatVersion int                     `json:"format-version" ub:"format-version"`
	Stack         string                  `json:"stack"          ub:"stack"`
	Action        string                  `json:"action"         ub:"action"`
	File          filechange.Change       `json:"file"           ub:"file"`
	Factory       factoryIdentity         `json:"factory"        ub:"factory"`
	Diagnostics   []diagnostic.Diagnostic `json:"diagnostics"    ub:"diagnostics"`
}

type stateForceUnlockResult struct {
	Kind          string                  `json:"kind"           ub:"kind"`
	FormatVersion int                     `json:"format-version" ub:"format-version"`
	Factory       factoryIdentity         `json:"factory"        ub:"factory"`
	Stack         string                  `json:"stack"          ub:"stack"`
	Unlocked      bool                    `json:"unlocked"       ub:"unlocked"`
	Diagnostics   []diagnostic.Diagnostic `json:"diagnostics"    ub:"diagnostics"`
}

type stateMoveResult struct {
	Kind          string                  `json:"kind"           ub:"kind"`
	FormatVersion int                     `json:"format-version" ub:"format-version"`
	Factory       factoryIdentity         `json:"factory"        ub:"factory"`
	Stack         string                  `json:"stack"          ub:"stack"`
	OK            bool                    `json:"ok"             ub:"ok"`
	From          string                  `json:"from"           ub:"from"`
	To            string                  `json:"to"             ub:"to"`
	Moved         int                     `json:"moved"          ub:"moved"`
	StateRev      string                  `json:"state-rev"      ub:"state-rev"`
	Diagnostics   []diagnostic.Diagnostic `json:"diagnostics"    ub:"diagnostics"`
}

type stateRemoveResult struct {
	Kind          string                  `json:"kind"           ub:"kind"`
	FormatVersion int                     `json:"format-version" ub:"format-version"`
	Factory       factoryIdentity         `json:"factory"        ub:"factory"`
	Stack         string                  `json:"stack"          ub:"stack"`
	OK            bool                    `json:"ok"             ub:"ok"`
	Address       string                  `json:"address"        ub:"address"`
	StateRev      string                  `json:"state-rev"      ub:"state-rev"`
	Diagnostics   []diagnostic.Diagnostic `json:"diagnostics"    ub:"diagnostics"`
}

type stateGCResult struct {
	Kind           string                  `json:"kind"            ub:"kind"`
	FormatVersion  int                     `json:"format-version"  ub:"format-version"`
	Factory        factoryIdentity         `json:"factory"         ub:"factory"`
	Stack          string                  `json:"stack"           ub:"stack"`
	OK             bool                    `json:"ok"              ub:"ok"`
	Deleted        int                     `json:"deleted"         ub:"deleted"`
	Kept           int                     `json:"kept"            ub:"kept"`
	Current        *string                 `json:"current"         ub:"current"`
	FailedRevision *string                 `json:"failed-revision" ub:"failed-revision"`
	Diagnostics    []diagnostic.Diagnostic `json:"diagnostics"     ub:"diagnostics"`
}

type refreshResult struct {
	Kind          string                  `json:"kind"           ub:"kind"`
	FormatVersion int                     `json:"format-version" ub:"format-version"`
	Factory       factoryIdentity         `json:"factory"        ub:"factory"`
	Stack         string                  `json:"stack"          ub:"stack"`
	OK            bool                    `json:"ok"             ub:"ok"`
	Refreshed     int                     `json:"refreshed"      ub:"refreshed"`
	Removed       int                     `json:"removed"        ub:"removed"`
	StateRev      *string                 `json:"state-rev"      ub:"state-rev"`
	Diagnostics   []diagnostic.Diagnostic `json:"diagnostics"    ub:"diagnostics"`
}

func buildStateListResult(
	info Info,
	stack string,
	revision *string,
	snapshot *state.Snapshot,
	diagnostics []diagnostic.Diagnostic,
) (stateListResult, error) {
	result := stateListResult{
		Kind:          "state-list",
		FormatVersion: 1,
		Factory:       factoryIdentityFor(info),
		Stack:         stack,
		StateRev:      copyOptionalString(revision),
		Entries:       []stateEntrySummary{},
		Diagnostics:   diagnostic.Normalize(diagnostics),
	}
	if snapshot == nil {
		return result, nil
	}
	entries, err := sortedStateEntries(snapshot)
	if err != nil {
		return stateListResult{}, err
	}
	for _, entry := range entries {
		summary, err := buildStateEntrySummary(entry)
		if err != nil {
			return stateListResult{}, err
		}
		result.Entries = append(result.Entries, summary)
	}
	return result, nil
}

func buildStateEntryResult(
	info Info,
	stack string,
	revision string,
	entry *state.Entry,
	diagnostics []diagnostic.Diagnostic,
) (stateEntryResult, error) {
	if revision == "" {
		return stateEntryResult{}, fmt.Errorf("state entry: revision is required")
	}
	detail, err := buildStateEntryDetail(entry)
	if err != nil {
		return stateEntryResult{}, err
	}
	return stateEntryResult{
		Kind:          "state-entry",
		FormatVersion: 1,
		Factory:       factoryIdentityFor(info),
		Stack:         stack,
		StateRev:      revision,
		Entry:         detail,
		Diagnostics:   diagnostic.Normalize(diagnostics),
	}, nil
}

func buildStateSnapshotsResult(
	info Info,
	stack string,
	current *string,
	revisions []string,
	diagnostics []diagnostic.Diagnostic,
) stateSnapshotsResult {
	result := stateSnapshotsResult{
		Kind:          "state-snapshots",
		FormatVersion: 1,
		Factory:       factoryIdentityFor(info),
		Stack:         stack,
		Current:       copyOptionalString(current),
		Snapshots:     make([]stateSnapshotSummary, 0, len(revisions)),
		Diagnostics:   diagnostic.Normalize(diagnostics),
	}
	for _, revision := range revisions {
		result.Snapshots = append(result.Snapshots, stateSnapshotSummary{
			Revision: revision,
			Current:  current != nil && revision == *current,
		})
	}
	return result
}

func buildPinResult(
	info Info,
	stack string,
	action string,
	file filechange.Change,
	diagnostics []diagnostic.Diagnostic,
) (pinResult, error) {
	publicAction, err := publicPinAction(action)
	if err != nil {
		return pinResult{}, err
	}
	files, err := filechange.Compose([]filechange.Change{file})
	if err != nil {
		return pinResult{}, err
	}
	if len(files) != 1 {
		return pinResult{}, fmt.Errorf("pin result: file change is required")
	}
	return pinResult{
		Kind:          "pin-result",
		FormatVersion: 1,
		Stack:         stack,
		Action:        publicAction,
		File:          files[0],
		Factory:       factoryIdentityFor(info),
		Diagnostics:   diagnostic.Normalize(diagnostics),
	}, nil
}

func buildStateForceUnlockResult(
	info Info,
	stack string,
	diagnostics []diagnostic.Diagnostic,
) stateForceUnlockResult {
	return stateForceUnlockResult{
		Kind:          "state-force-unlock-result",
		FormatVersion: 1,
		Factory:       factoryIdentityFor(info),
		Stack:         stack,
		Unlocked:      true,
		Diagnostics:   diagnostic.Normalize(diagnostics),
	}
}

func buildStateMoveResult(
	info Info,
	stack string,
	ok bool,
	from string,
	to string,
	moved int,
	revision string,
	diagnostics []diagnostic.Diagnostic,
) (stateMoveResult, error) {
	if revision == "" {
		return stateMoveResult{}, fmt.Errorf("state move result: revision is required")
	}
	return stateMoveResult{
		Kind:          "state-move-result",
		FormatVersion: 1,
		Factory:       factoryIdentityFor(info),
		Stack:         stack,
		OK:            ok,
		From:          from,
		To:            to,
		Moved:         moved,
		StateRev:      revision,
		Diagnostics:   diagnostic.Normalize(diagnostics),
	}, nil
}

func buildStateRemoveResult(
	info Info,
	stack string,
	ok bool,
	address string,
	revision string,
	diagnostics []diagnostic.Diagnostic,
) (stateRemoveResult, error) {
	if revision == "" {
		return stateRemoveResult{}, fmt.Errorf("state remove result: revision is required")
	}
	return stateRemoveResult{
		Kind:          "state-remove-result",
		FormatVersion: 1,
		Factory:       factoryIdentityFor(info),
		Stack:         stack,
		OK:            ok,
		Address:       address,
		StateRev:      revision,
		Diagnostics:   diagnostic.Normalize(diagnostics),
	}, nil
}

func buildStateGCResult(
	info Info,
	stack string,
	ok bool,
	deleted int,
	kept int,
	current *string,
	failedRevision *string,
	diagnostics []diagnostic.Diagnostic,
) stateGCResult {
	return stateGCResult{
		Kind:           "state-gc-result",
		FormatVersion:  1,
		Factory:        factoryIdentityFor(info),
		Stack:          stack,
		OK:             ok,
		Deleted:        deleted,
		Kept:           kept,
		Current:        copyOptionalString(current),
		FailedRevision: copyOptionalString(failedRevision),
		Diagnostics:    diagnostic.Normalize(diagnostics),
	}
}

func buildRefreshResult(
	info Info,
	stack string,
	ok bool,
	refreshed int,
	removed int,
	revision *string,
	diagnostics []diagnostic.Diagnostic,
) refreshResult {
	return refreshResult{
		Kind:          "refresh-result",
		FormatVersion: 1,
		Factory:       factoryIdentityFor(info),
		Stack:         stack,
		OK:            ok,
		Refreshed:     refreshed,
		Removed:       removed,
		StateRev:      copyOptionalString(revision),
		Diagnostics:   diagnostic.Normalize(diagnostics),
	}
}

func buildStateEntrySummary(entry *state.Entry) (stateEntrySummary, error) {
	binding, err := publicStateBinding(entry)
	if err != nil {
		return stateEntrySummary{}, err
	}
	return stateEntrySummary{
		Address:   entry.Address,
		EntryType: string(entry.Type),
		Category:  entry.Category,
		Binding:   binding,
	}, nil
}

func buildStateEntryDetail(entry *state.Entry) (stateEntryDetail, error) {
	summary, err := buildStateEntrySummary(entry)
	if err != nil {
		return stateEntryDetail{}, err
	}
	inputs := maskStateValues(entry.Inputs, entry.SensitiveInputs)
	outputs := maskStateValues(entry.Outputs, entry.SensitiveOutputs)
	dependsOn := sortedStrings(entry.DependsOn)
	sensitiveInputs := sortedStrings(entry.SensitiveInputs)
	sensitiveOutputs := sortedStrings(entry.SensitiveOutputs)
	var triggerHash *string
	if entry.TriggerHash != "" {
		value := entry.TriggerHash
		triggerHash = &value
	}
	return stateEntryDetail{
		Address:          summary.Address,
		EntryType:        summary.EntryType,
		Category:         summary.Category,
		Binding:          summary.Binding,
		SchemaVersion:    entry.SchemaVersion,
		TriggerHash:      triggerHash,
		Inputs:           inputs,
		Outputs:          outputs,
		DependsOn:        dependsOn,
		SensitiveInputs:  sensitiveInputs,
		SensitiveOutputs: sensitiveOutputs,
	}, nil
}

func publicStateBinding(entry *state.Entry) (graphprint.Binding, error) {
	if entry == nil {
		return graphprint.Binding{}, fmt.Errorf("state entry is required")
	}
	if entry.Binding == nil || entry.Binding.Alias == "" || entry.Binding.Export == "" {
		return graphprint.Binding{}, fmt.Errorf(
			"state entry %q is missing a valid binding", entry.Address,
		)
	}
	var libraryPath *string
	if entry.Binding.LibraryPath != "" {
		value := entry.Binding.LibraryPath
		libraryPath = &value
	}
	return graphprint.Binding{
		LibraryPath: libraryPath,
		Alias:       entry.Binding.Alias,
		Export:      entry.Binding.Export,
	}, nil
}

func maskStateValues(values map[string]any, sensitive []string) map[string]any {
	masked := make(map[string]any, len(values))
	for key, value := range values {
		masked[key] = value
	}
	for _, key := range sensitive {
		if _, ok := masked[key]; ok {
			masked[key] = sensitivePlaceholder
		}
	}
	return masked
}

func sortedStrings(values []string) []string {
	result := slices.Clone(values)
	if result == nil {
		result = []string{}
	}
	slices.Sort(result)
	return result
}

func copyOptionalString(value *string) *string {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func publicPinAction(action string) (string, error) {
	switch action {
	case pinActionAddedFactoryBlock:
		return "added-factory-block", nil
	case pinActionAddedPin:
		return "added-pin-block", nil
	case pinActionAddedSupportedVersions:
		return "added-supported-versions", nil
	case pinActionAppendedEntry:
		return "appended-entry", nil
	case pinActionAlreadyPinned:
		return "already-pinned", nil
	default:
		return "", fmt.Errorf("pin result: unknown action %q", action)
	}
}
