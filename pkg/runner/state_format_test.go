package runner

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/cmdout"
	"github.com/cloudboss/unobin/pkg/diagnostic"
	"github.com/cloudboss/unobin/pkg/filechange"
	"github.com/cloudboss/unobin/pkg/sdk/state"
)

func TestStateInspectionResultsGolden(t *testing.T) {
	input, err := os.ReadFile("testdata/state-contract-input.json")
	require.NoError(t, err)
	var snapshot state.Snapshot
	require.NoError(t, json.Unmarshal(input, &snapshot))
	info := Info{
		FactoryName: "appdeploy", FactoryVersion: "v0.1.0",
		ContentRevision: "abc123def456", LibraryPath: "example.com/appdeploy",
	}
	revision := "rev-2"
	diagnostics := []diagnostic.Diagnostic{{
		Code: "unobin.test", Severity: diagnostic.SeverityInfo, Message: "notice",
	}}
	list, err := buildStateListResult(info, "dev", &revision, &snapshot, diagnostics)
	require.NoError(t, err)
	entry, err := buildStateEntryResult(info, "dev", revision, snapshot.Entries[0], diagnostics)
	require.NoError(t, err)
	snapshots := buildStateSnapshotsResult(
		info, "dev", &revision, []string{"rev-1", "rev-2"}, diagnostics,
	)
	pin, err := buildPinResult(
		info, "dev", pinActionAppendedEntry,
		filechange.Change{Path: "dev.ub", Action: filechange.ActionUpdated}, diagnostics,
	)
	require.NoError(t, err)
	forceUnlock := buildStateForceUnlockResult(info, "dev", diagnostics)
	documents := []any{list, entry, snapshots, pin, forceUnlock}

	for _, tc := range []struct {
		format cmdout.Format
		path   string
	}{
		{format: cmdout.FormatJSON, path: "testdata/state-contracts.jsonl"},
		{format: cmdout.FormatUnobin, path: "testdata/state-contracts-unobin.stdout"},
	} {
		var got bytes.Buffer
		for _, document := range documents {
			require.NoError(t, cmdout.WriteDocument(&got, tc.format, document))
		}
		want, err := os.ReadFile(tc.path)
		require.NoError(t, err)
		require.Equal(t, string(want), got.String())
	}
}

func TestStateMutationResultsGolden(t *testing.T) {
	info := Info{
		FactoryName: "appdeploy", FactoryVersion: "v0.1.0",
		ContentRevision: "abc123def456", LibraryPath: "example.com/appdeploy",
	}
	revision := "rev-3"
	failedRevision := "rev-1"
	diagnostics := []diagnostic.Diagnostic{{
		Code: "unobin.state.unlock", Severity: diagnostic.SeverityError,
		Message: "release lock: unlock failed",
	}}
	move, err := buildStateMoveResult(
		info, "dev", false, "resource.old", "resource.new", 1, revision, diagnostics,
	)
	require.NoError(t, err)
	remove, err := buildStateRemoveResult(
		info, "dev", true, "resource.old", revision, nil,
	)
	require.NoError(t, err)
	gc := buildStateGCResult(
		info, "dev", false, 2, 3, &revision, &failedRevision, diagnostics,
	)
	refresh := buildRefreshResult(info, "dev", false, 3, 1, &revision, diagnostics)
	documents := []any{move, remove, gc, refresh}
	for _, tc := range []struct {
		format cmdout.Format
		path   string
	}{
		{format: cmdout.FormatJSON, path: "testdata/state-mutations.jsonl"},
		{format: cmdout.FormatUnobin, path: "testdata/state-mutations-unobin.stdout"},
	} {
		var got bytes.Buffer
		for _, document := range documents {
			require.NoError(t, cmdout.WriteDocument(&got, tc.format, document))
		}
		want, err := os.ReadFile(tc.path)
		require.NoError(t, err)
		require.Equal(t, string(want), got.String())
	}
}
