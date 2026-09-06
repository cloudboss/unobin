package resolve

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCanonicalUBLibraryPath(t *testing.T) {
	for _, test := range []struct {
		name   string
		ref    ImportRef
		parent string
		repo   string
		want   string
	}{
		{"local root", &LocalImport{Path: "./libraries/shared"}, "", "",
			"local:libraries/shared"},
		{"local nested", &LocalImport{Path: "../shared"}, "local:libraries/wrapper", "",
			"local:libraries/shared"},
		{"remote root", &RemoteImport{URL: "example.com/lib", Subdir: "libraries/shared"}, "", "",
			"example.com/lib//libraries/shared"},
		{"remote nested", &LocalImport{Path: "../shared"},
			"example.com/lib//libraries/wrapper", "example.com/lib",
			"example.com/lib//libraries/shared"},
		{"remote repository root", &LocalImport{Path: "../.."},
			"example.com/lib//libraries/wrapper", "example.com/lib", "example.com/lib"},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, checkout := range []string{"/tmp/first", "/tmp/second"} {
				got := canonicalUBLibraryPath(test.ref, &Source{Path: checkout}, test.parent, test.repo)
				require.Equal(t, test.want, got)
			}
		})
	}
}
