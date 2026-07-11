package runner

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/cmdout"
	"github.com/cloudboss/unobin/pkg/diagnostic"
)

func TestOutputResultsFormatGolden(t *testing.T) {
	info := Info{
		FactoryName: "appdeploy", FactoryVersion: "v0.1.0",
		ContentRevision: "abc123def456", LibraryPath: "example.com/appdeploy",
	}
	outputs := map[string]any{
		"a-secret": "must-not-appear",
		"m-list":   []any{"one", int64(2)},
		"z-public": map[string]any{"enabled": true},
	}
	sensitive := map[string]bool{"missing-secret": true, "a-secret": true}
	diagnostics := []diagnostic.Diagnostic{{
		Code: "unobin.test", Severity: diagnostic.SeverityInfo, Message: "notice",
	}}
	documents := []any{
		buildOutputsResult(info, "dev", outputs, sensitive, diagnostics),
		buildOutputResult(info, "dev", "z-public", outputs["z-public"], false, diagnostics),
		buildOutputResult(info, "dev", "a-secret", outputs["a-secret"], true, diagnostics),
		buildOutputsResult(info, "empty", nil, nil, nil),
	}
	for _, tc := range []struct {
		format cmdout.Format
		path   string
	}{
		{format: cmdout.FormatJSON, path: "testdata/output-contracts.jsonl"},
		{format: cmdout.FormatUnobin, path: "testdata/output-contracts-unobin.stdout"},
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
