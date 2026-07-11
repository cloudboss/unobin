package ui

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type browserGolden struct {
	Cases []browserCaseGolden `json:"cases"`
}

type browserCaseGolden struct {
	Name  string `json:"name"`
	OK    bool   `json:"ok"`
	Error string `json:"error"`
}

func TestOpenBrowserContextGolden(t *testing.T) {
	helper := func(mode string) []string {
		return []string{os.Args[0], "-test.run=TestBrowserHelperProcess", "--", mode}
	}
	result := browserGolden{}
	for _, tc := range []struct {
		name     string
		commands [][]string
		timeout  time.Duration
		wait     time.Duration
	}{
		{name: "successful command", commands: [][]string{helper("success")}, wait: time.Second},
		{
			name:     "fallback command",
			commands: [][]string{helper("failure"), helper("success")}, wait: time.Second,
		},
		{name: "foreground browser", commands: [][]string{helper("wait")}, wait: time.Millisecond},
		{
			name: "context deadline", commands: [][]string{helper("wait")},
			timeout: 10 * time.Millisecond, wait: time.Second,
		},
		{name: "all commands fail", commands: [][]string{helper("failure")}, wait: time.Second},
	} {
		ctx := context.Background()
		cancel := func() {}
		if tc.timeout > 0 {
			ctx, cancel = context.WithTimeout(ctx, tc.timeout)
		}
		err := openBrowserContext(ctx, "http://127.0.0.1/run", tc.commands, tc.wait)
		cancel()
		entry := browserCaseGolden{Name: tc.name, OK: err == nil}
		if err != nil {
			entry.Error = err.Error()
		}
		result.Cases = append(result.Cases, entry)
	}
	got, err := json.MarshalIndent(result, "", "  ")
	require.NoError(t, err)
	got = append(got, '\n')
	want, err := os.ReadFile("testdata/browser.json")
	require.NoError(t, err)
	require.Equal(t, string(want), string(got))
}

func TestBrowserHelperProcess(t *testing.T) {
	mode := ""
	for i, arg := range os.Args {
		if arg == "--" && i+1 < len(os.Args) {
			mode = os.Args[i+1]
			break
		}
	}
	if mode == "" {
		return
	}
	switch mode {
	case "success":
		os.Exit(0)
	case "failure":
		os.Exit(7)
	case "wait":
		time.Sleep(100 * time.Millisecond)
		os.Exit(0)
	default:
		os.Exit(9)
	}
}
