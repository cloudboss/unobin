package resolve

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseImportRefLocal(t *testing.T) {
	cases := []string{
		".",
		"..",
		"./local-libraries/foo",
		"../shared/bar",
		"/abs/path/library",
	}
	for _, c := range cases {
		t.Run(c, func(t *testing.T) {
			ref, err := ParseImportRef(c)
			require.NoError(t, err)
			local, ok := ref.(*LocalImport)
			require.True(t, ok, "got %T", ref)
			require.Equal(t, c, local.Path)
		})
	}
}

func TestParseImportRefRemoteNoSubdir(t *testing.T) {
	cases := []struct {
		in      string
		url     string
		version string
	}{
		{"github.com/cloudboss/unobin@v1.2.3", "github.com/cloudboss/unobin", "v1.2.3"},
		{"gitlab.com/group/repo@v0.1.0", "gitlab.com/group/repo", "v0.1.0"},
		{"git.example.com/team/repo@v9.9.9", "git.example.com/team/repo", "v9.9.9"},
		{"github.com/owner/repo/deep/path@v1.0.0", "github.com/owner/repo/deep/path", "v1.0.0"},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			ref, err := ParseImportRef(c.in)
			require.NoError(t, err)
			rem, ok := ref.(*RemoteImport)
			require.True(t, ok, "got %T", ref)
			require.Equal(t, c.url, rem.URL)
			require.Equal(t, "", rem.Subdir)
			require.Equal(t, c.version, rem.Version)
		})
	}
}

func TestParseImportRefDoubleSlashSubdir(t *testing.T) {
	cases := []struct {
		in      string
		url     string
		subdir  string
		version string
	}{
		{
			in:      "github.com/cloudboss/unobin-libraries//aws@v0.5.0",
			url:     "github.com/cloudboss/unobin-libraries",
			subdir:  "aws",
			version: "v0.5.0",
		},
		{
			in:      "git.example.com/team/repo//libraries/network@v1.2.3",
			url:     "git.example.com/team/repo",
			subdir:  "libraries/network",
			version: "v1.2.3",
		},
		{
			in:      "github.com/owner/repo//deep/sub@v1.0.0",
			url:     "github.com/owner/repo",
			subdir:  "deep/sub",
			version: "v1.0.0",
		},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			ref, err := ParseImportRef(c.in)
			require.NoError(t, err)
			rem, ok := ref.(*RemoteImport)
			require.True(t, ok, "got %T", ref)
			require.Equal(t, c.url, rem.URL)
			require.Equal(t, c.subdir, rem.Subdir)
			require.Equal(t, c.version, rem.Version)
		})
	}
}

func TestParseImportRefRejects(t *testing.T) {
	cases := []struct {
		in      string
		wantSub string
	}{
		{"", "empty"},
		{"github.com/owner/repo@", "empty version"},
		{"github.com@v1.0.0", "host and a path"},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			_, err := ParseImportRef(c.in)
			require.Error(t, err)
			require.True(t, strings.Contains(err.Error(), c.wantSub),
				"error %q should contain %q", err.Error(), c.wantSub)
		})
	}
}

func TestParseImportRefVersionWithAtSign(t *testing.T) {
	// LastIndex on `@` so a tag like `v1.0.0@beta` (unusual but legal) takes
	// the rightmost split.
	ref, err := ParseImportRef("github.com/owner/repo@v1.0.0-beta")
	require.NoError(t, err)
	rem := ref.(*RemoteImport)
	require.Equal(t, "v1.0.0-beta", rem.Version)
}

func TestParseImportRefVersionless(t *testing.T) {
	cases := []struct {
		in     string
		url    string
		subdir string
	}{
		{"github.com/cloudboss/unobin", "github.com/cloudboss/unobin", ""},
		{"github.com/cloudboss/unobin//pkg/libraries/core",
			"github.com/cloudboss/unobin", "pkg/libraries/core"},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			ref, err := ParseImportRef(c.in)
			require.NoError(t, err)
			rem := ref.(*RemoteImport)
			require.Equal(t, c.url, rem.URL)
			require.Equal(t, c.subdir, rem.Subdir)
			require.Empty(t, rem.Version)
		})
	}
}
