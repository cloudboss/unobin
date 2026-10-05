package sourcecheck

import (
	"go/token"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUBPackageIDsAvoidAliasCollisions(t *testing.T) {
	ids := newUBPackageIDs()
	tests := []struct {
		key   string
		alias string
		want  string
	}{
		{
			key:   "local:/tmp/app/a",
			alias: "a",
			want:  "a",
		},
		{
			key:   "local:/tmp/app/b",
			alias: "a",
			want:  "a_c82f8103",
		},
		{
			key:   "remote:example.com/x//lib@v1.0.0",
			alias: "lib",
			want:  "lib",
		},
		{
			key:   "remote:example.com/y//lib@v1.0.0",
			alias: "lib",
			want:  "lib_8f267c21",
		},
	}

	gotByKey := map[string]string{}
	for _, tt := range tests {
		got := ids.ID(tt.alias, tt.key)
		repeated := ids.ID(tt.alias, tt.key)

		require.Equal(t, tt.want, got)
		require.Equal(t, got, repeated)
		require.True(t, isValidGoPackageIdent(got), "%s is not a Go package identifier", got)
		gotByKey[tt.key] = got
	}

	seen := map[string]string{}
	for key, got := range gotByKey {
		previous, ok := seen[got]
		require.False(t, ok, "%s and %s both use %s", previous, key, got)
		seen[got] = key
	}

	again := newUBPackageIDs()
	for _, tt := range tests {
		require.Equal(t, gotByKey[tt.key], again.ID(tt.alias, tt.key))
	}
}

func TestUBPackageIDsReuseCanonicalKey(t *testing.T) {
	ids := newUBPackageIDs()
	key := "local:/tmp/app/library"
	first := ids.ID("original", key)

	require.Equal(t, "original", first)
	require.Equal(t, first, ids.ID("renamed", key))
	require.Equal(t, "renamed", ids.ID("renamed", "local:/tmp/app/other"))
}

func TestSanitizeGoPackageID(t *testing.T) {
	tests := []struct {
		alias string
		want  string
	}{
		{alias: "", want: "x"},
		{alias: "9cats", want: "_9cats"},
		{alias: "type", want: "_type"},
		{alias: "foo-bar", want: "foo_bar"},
		{alias: "foo.bar", want: "foo_bar"},
		{alias: "naïve", want: "na_ve"},
		{alias: "valid_123", want: "valid_123"},
	}
	for _, tt := range tests {
		t.Run(tt.alias, func(t *testing.T) {
			got := sanitizeGoPackageID(tt.alias)
			require.Equal(t, tt.want, got)
			require.True(t, isValidGoPackageIdent(got))
		})
	}
}

func isValidGoPackageIdent(s string) bool {
	if s == "" || token.Lookup(s).IsKeyword() {
		return false
	}
	for i, r := range s {
		if r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' {
			continue
		}
		if i > 0 && r >= '0' && r <= '9' {
			continue
		}
		return false
	}
	return true
}
