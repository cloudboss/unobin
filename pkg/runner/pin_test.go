package runner

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/lang"
)

// TestPinFile asserts the canonical form of pinFile's output, since the
// splice result is a formatter draft and reaches disk only through
// WriteCanonical. The already-pinned case asserts raw bytes instead:
// that path leaves the file untouched.
func TestPinFile(t *testing.T) {
	const (
		libraryPath = "github.com/cloudboss/cluster-deploy"
		version     = "v0.3.0"
		revision    = "fedcba"
	)
	tests := []struct {
		name   string
		src    string
		want   string
		action string
	}{
		{
			name: "no factory block",
			src: `locals: {
  message: 'hi'
}
`,
			want: `factory: {
  pin: {
    library-path: 'github.com/cloudboss/cluster-deploy'
    supported-versions: [
      { version: 'v0.3.0', content-revision: 'fedcba' },
    ]
  }
}

locals: {
  message: 'hi'
}
`,
			action: pinActionAddedFactoryBlock,
		},
		{
			name: "empty file",
			src:  ``,
			want: `factory: {
  pin: {
    library-path: 'github.com/cloudboss/cluster-deploy'
    supported-versions: [
      { version: 'v0.3.0', content-revision: 'fedcba' },
    ]
  }
}
`,
			action: pinActionAddedFactoryBlock,
		},
		{
			name: "factory block without pin",
			src: `factory: {
  inputs: { message: 'hi' }
}
`,
			want: `factory: {
  inputs: { message: 'hi' }
  pin: {
    library-path: 'github.com/cloudboss/cluster-deploy'
    supported-versions: [
      { version: 'v0.3.0', content-revision: 'fedcba' },
    ]
  }
}
`,
			action: pinActionAddedPin,
		},
		{
			name: "single-line factory block without pin",
			src: `factory: { inputs: { message: 'hi' } }
`,
			want: `factory: {
  inputs: { message: 'hi' }
  pin: {
    library-path: 'github.com/cloudboss/cluster-deploy'
    supported-versions: [
      { version: 'v0.3.0', content-revision: 'fedcba' },
    ]
  }
}
`,
			action: pinActionAddedPin,
		},
		{
			name: "source stack factory block without pin",
			src: `stack: {
  factory: {
    inputs: { message: 'hi' }
  }

  state: local { path: '.unobin/state' }
  encryption: noop {}
}
`,
			want: `stack: {
  factory: {
    inputs: { message: 'hi' }
    pin: {
      library-path: 'github.com/cloudboss/cluster-deploy'
      supported-versions: [
        { version: 'v0.3.0', content-revision: 'fedcba' },
      ]
    }
  }

  state: local {
    path: '.unobin/state'
  }
  encryption: noop {}
}
`,
			action: pinActionAddedPin,
		},
		{
			name: "pin block missing supported-versions",
			src: `factory: {
  pin: {
    library-path: 'github.com/cloudboss/cluster-deploy'
  }
}
`,
			want: `factory: {
  pin: {
    library-path: 'github.com/cloudboss/cluster-deploy'
    supported-versions: [
      { version: 'v0.3.0', content-revision: 'fedcba' },
    ]
  }
}
`,
			action: pinActionAddedSupportedVersions,
		},
		{
			name: "empty pin block",
			src: `factory: {
  pin: {}
}
`,
			want: `factory: {
  pin: {
    library-path: 'github.com/cloudboss/cluster-deploy'
    supported-versions: [
      { version: 'v0.3.0', content-revision: 'fedcba' },
    ]
  }
}
`,
			action: pinActionAddedSupportedVersions,
		},
		{
			name: "empty supported-versions list",
			src: `factory: {
  pin: {
    library-path: 'github.com/cloudboss/cluster-deploy'
    supported-versions: []
  }
}
`,
			want: `factory: {
  pin: {
    library-path: 'github.com/cloudboss/cluster-deploy'
    supported-versions: [
      { version: 'v0.3.0', content-revision: 'fedcba' },
    ]
  }
}
`,
			action: pinActionAppendedEntry,
		},
		{
			name: "existing entries without a trailing comma",
			src: `factory: {
  pin: {
    library-path: 'github.com/cloudboss/cluster-deploy'
    supported-versions: [
      { version: 'v0.1.0', content-revision: 'aaa' },
      { version: 'v0.2.0', content-revision: 'bbb' }
    ]
  }
}
`,
			want: `factory: {
  pin: {
    library-path: 'github.com/cloudboss/cluster-deploy'
    supported-versions: [
      { version: 'v0.1.0', content-revision: 'aaa' },
      { version: 'v0.2.0', content-revision: 'bbb' },
      { version: 'v0.3.0', content-revision: 'fedcba' },
    ]
  }
}
`,
			action: pinActionAppendedEntry,
		},
		{
			name: "existing entries with a trailing comma",
			src: `factory: {
  pin: {
    library-path: 'github.com/cloudboss/cluster-deploy'
    supported-versions: [
      { version: 'v0.1.0', content-revision: 'aaa' },
    ]
  }
}
`,
			want: `factory: {
  pin: {
    library-path: 'github.com/cloudboss/cluster-deploy'
    supported-versions: [
      { version: 'v0.1.0', content-revision: 'aaa' },
      { version: 'v0.3.0', content-revision: 'fedcba' },
    ]
  }
}
`,
			action: pinActionAppendedEntry,
		},
		{
			name: "idempotent when entry already present",
			src: `factory: {
  pin: {
    library-path: 'github.com/cloudboss/cluster-deploy'
    supported-versions: [
      { version: 'v0.3.0', content-revision: 'fedcba' },
    ]
  }
}
`,
			want: `factory: {
  pin: {
    library-path: 'github.com/cloudboss/cluster-deploy'
    supported-versions: [
      { version: 'v0.3.0', content-revision: 'fedcba' },
    ]
  }
}
`,
			action: pinActionAlreadyPinned,
		},
		{
			name: "inline supported-versions with one entry",
			src: `factory: {
  pin: {
    library-path: 'github.com/cloudboss/cluster-deploy'
    supported-versions: [{ version: 'v0.1.0', content-revision: 'aaa' }]
  }
}
`,
			want: `factory: {
  pin: {
    library-path: 'github.com/cloudboss/cluster-deploy'
    supported-versions: [
      { version: 'v0.1.0', content-revision: 'aaa' },
      { version: 'v0.3.0', content-revision: 'fedcba' },
    ]
  }
}
`,
			action: pinActionAppendedEntry,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, action, err := pinFile([]byte(tt.src), libraryPath, version, revision)
			require.NoError(t, err)
			assert.Equal(t, tt.action, action)
			if tt.action == pinActionAlreadyPinned {
				assert.Equal(t, tt.src, string(got))
				return
			}
			canonical, err := lang.Canonicalize("stack.ub", got)
			require.NoError(t, err, "pinFile output failed to parse")
			assert.Equal(t, tt.want, string(canonical))
		})
	}
}

func TestPinFileRejectsLibraryPathMismatch(t *testing.T) {
	src := []byte(`factory: {
  pin: {
    library-path: 'github.com/cloudboss/other'
    supported-versions: []
  }
}
`)
	_, _, err := pinFile(src, "github.com/cloudboss/cluster-deploy", "v0.1.0", "aaa")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "library-path")
}

// TestPinFileRejectsInvalidConfig proves pin refuses to splice into a
// config the validator rejects, instead of stacking a pin block beside
// keys it does not understand.
func TestPinFileRejectsInvalidConfig(t *testing.T) {
	src := []byte(`factory: {
  supported-versions: []
}
`)
	_, _, err := pinFile(src, "github.com/cloudboss/cluster-deploy", "v0.1.0", "aaa")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a valid factory key")
}

// TestPinWritesCanonicalFile proves the written stack file is reformatted as a
// whole, not just the spliced entry: an operator's odd indentation in an
// untouched block comes out canonical too.
func TestPinWritesCanonicalFile(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "dev.ub")
	require.NoError(t, os.WriteFile(configPath,
		[]byte("locals: {\n    message:   'hi'\n}\n"), 0o644))

	info := Info{
		LibraryPath:     "github.com/cloudboss/cluster-deploy",
		FactoryVersion:  "v0.3.0",
		ContentRevision: "fedcba",
	}
	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	require.NoError(t, doPin(cmd, info, configPath, "", ""))

	got, err := os.ReadFile(configPath)
	require.NoError(t, err)
	canonical, err := lang.Canonicalize("stack.ub", got)
	require.NoError(t, err)
	assert.Equal(t, string(canonical), string(got), "pinned stack file should be canonical")
	assert.NotContains(t, string(got), "    message", "operator indentation should be normalized")
}

func TestPinFilePreservesTrailingContent(t *testing.T) {
	src := []byte(`factory: {
  pin: {
    library-path: 'github.com/cloudboss/cluster-deploy'
    supported-versions: [
      { version: 'v0.1.0', content-revision: 'aaa' }
    ]
  }
}

state: {
  @backend: local
  path:     '.unobin/state'
}
`)
	want := `factory: {
  pin: {
    library-path: 'github.com/cloudboss/cluster-deploy'
    supported-versions: [
      { version: 'v0.1.0', content-revision: 'aaa' },
      { version: 'v0.3.0', content-revision: 'fedcba' },
    ]
  }
}

state: {
  @backend: local
  path:     '.unobin/state'
}
`
	got, action, err := pinFile(src, "github.com/cloudboss/cluster-deploy", "v0.3.0", "fedcba")
	require.NoError(t, err)
	assert.Equal(t, pinActionAppendedEntry, action)
	canonical, err := lang.Canonicalize("stack.ub", got)
	require.NoError(t, err)
	assert.Equal(t, want, string(canonical))
}
