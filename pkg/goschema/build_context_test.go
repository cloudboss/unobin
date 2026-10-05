package goschema

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/typecheck"
)

func TestSourceIndexCacheUsesGoTargetContext(t *testing.T) {
	dir := filepath.Join("..", "gopackage", "testdata", "buildcontext")
	cache := NewSourceIndexCache()
	for _, tt := range []struct {
		name     string
		os       string
		arch     string
		cgo      string
		tags     string
		amd      string
		platform string
		cpu      string
		feature  string
		native   string
		tuning   string
	}{
		{name: "linux", os: "linux", arch: "amd64", cgo: "0", amd: "v1",
			platform: "linux", cpu: "amd", feature: "ordinary", native: "fallback", tuning: "low"},
		{name: "windows", os: "windows", arch: "arm64", cgo: "0",
			platform: "windows", cpu: "arm", feature: "ordinary", native: "fallback", tuning: "low"},
		{name: "tags", os: "linux", arch: "amd64", cgo: "0", amd: "v1", tags: "-tags=special",
			platform: "linux", cpu: "amd", feature: "special", native: "fallback", tuning: "low"},
		{name: "cgo and feature", os: "linux", arch: "amd64", cgo: "1", amd: "v2",
			platform: "linux", cpu: "amd", feature: "ordinary", native: "native", tuning: "high"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("GOOS", tt.os)
			t.Setenv("GOARCH", tt.arch)
			t.Setenv("CGO_ENABLED", tt.cgo)
			t.Setenv("GOFLAGS", tt.tags)
			t.Setenv("GOAMD64", tt.amd)
			schema, index, warnings, err := cache.Read(dir)
			require.NoError(t, err)
			require.Empty(t, warnings)
			query := schema.DataSources["query"]
			for name, field := range map[string]string{
				"platform": tt.platform, "cpu": tt.cpu, "feature": tt.feature,
				"native": tt.native, "tuning": tt.tuning,
			} {
				got := query.Inputs[name]
				require.Equal(t, typecheck.Object, got.Kind)
				require.Len(t, got.Fields, 1)
				require.Equal(t, field, got.Fields[0].Name)
				require.Contains(t, index.InputFields["data-source"]["query"], name+"."+field)
			}
			require.Contains(t, query.Outputs, tt.platform)
			requireLocationPrefix(t, index.OutputTypes["data-source"]["query"],
				"output_"+tt.os+".go", "Output")
		})
	}
}

func TestSourceIndexKeepsPhysicalLocationsWithLineDirectives(t *testing.T) {
	t.Setenv("GOOS", "linux")
	t.Setenv("GOARCH", "amd64")
	t.Setenv("CGO_ENABLED", "0")
	dir := t.TempDir()
	require.NoError(t, os.CopyFS(dir, os.DirFS(
		filepath.Join("..", "gopackage", "testdata", "buildcontext"))))
	path := filepath.Join(dir, "library.go")
	body, err := os.ReadFile(path)
	require.NoError(t, err)
	updated := strings.Replace(string(body), "type Query struct",
		"//line virtual.go:500\ntype Query struct", 1)
	require.NotEqual(t, string(body), updated)
	require.NoError(t, os.WriteFile(path, []byte(updated), 0o644))
	_, index, _, err := ReadWithIndex(dir)
	require.NoError(t, err)
	requireLocationPrefix(t, index.InputTypes["data-source"]["query"], "library.go", "Query")
	requireLocationPrefix(t, index.InputFields["data-source"]["query"]["platform"],
		"library.go", "Platform")
}
