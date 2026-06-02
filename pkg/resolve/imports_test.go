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
		in  string
		url string
	}{
		{"github.com/cloudboss/unobin", "github.com/cloudboss/unobin"},
		{"gitlab.com/group/repo", "gitlab.com/group/repo"},
		{"git.example.com/team/repo", "git.example.com/team/repo"},
		{"github.com/owner/repo/deep/path", "github.com/owner/repo/deep/path"},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			ref, err := ParseImportRef(c.in)
			require.NoError(t, err)
			rem, ok := ref.(*RemoteImport)
			require.True(t, ok, "got %T", ref)
			require.Equal(t, c.url, rem.URL)
			require.Equal(t, "", rem.Subdir)
			require.Empty(t, rem.Version)
		})
	}
}

func TestParseImportRefDoubleSlashSubdir(t *testing.T) {
	cases := []struct {
		in     string
		url    string
		subdir string
	}{
		{
			in:     "github.com/cloudboss/unobin-libraries//aws",
			url:    "github.com/cloudboss/unobin-libraries",
			subdir: "aws",
		},
		{
			in:     "git.example.com/team/repo//libraries/network",
			url:    "git.example.com/team/repo",
			subdir: "libraries/network",
		},
		{
			in:     "github.com/owner/repo//deep/sub",
			url:    "github.com/owner/repo",
			subdir: "deep/sub",
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
			require.Empty(t, rem.Version)
		})
	}
}

func TestParseImportRefRejects(t *testing.T) {
	cases := []struct {
		in      string
		wantSub string
	}{
		{"", "empty"},
		{"github.com", "host and a path"},
		{"github.com/owner/repo@v1.0.0", "invalid character"},
		{"github.com/owner/repo//aws@v0.5.0", "invalid character"},
		{"github.com/owner/repo!", "invalid character"},
		{"github.com/owner/repo?x", "invalid character"},
		{"github.com/owner/re|po", "invalid character"},
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
