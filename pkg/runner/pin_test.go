package runner

import (
	"testing"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
			name: "no stack block, file with inputs only",
			src: `inputs: {
  message: 'hi'
}
`,
			want: `stack: {
  library-path: 'github.com/cloudboss/cluster-deploy'
  supported-versions: [
    { version: 'v0.3.0', content-revision: 'fedcba' },
  ]
}

inputs: {
  message: 'hi'
}
`,
			action: pinActionAddedStackBlock,
		},
		{
			name: "empty file",
			src:  ``,
			want: `stack: {
  library-path: 'github.com/cloudboss/cluster-deploy'
  supported-versions: [
    { version: 'v0.3.0', content-revision: 'fedcba' },
  ]
}
`,
			action: pinActionAddedStackBlock,
		},
		{
			name: "stack block missing supported-versions",
			src: `stack: {
  library-path: 'github.com/cloudboss/cluster-deploy'
}
`,
			want: `stack: {
  library-path: 'github.com/cloudboss/cluster-deploy'
  supported-versions: [
    { version: 'v0.3.0', content-revision: 'fedcba' },
  ]
}
`,
			action: pinActionAddedSupportedVersions,
		},
		{
			name: "stack block missing library-path and supported-versions",
			src: `stack: {
}
`,
			want: `stack: {
  library-path: 'github.com/cloudboss/cluster-deploy'
  supported-versions: [
    { version: 'v0.3.0', content-revision: 'fedcba' },
  ]
}
`,
			action: pinActionAddedSupportedVersions,
		},
		{
			name: "empty stack block on a single line",
			src: `stack: {}
`,
			want: `stack: {
  library-path: 'github.com/cloudboss/cluster-deploy'
  supported-versions: [
    { version: 'v0.3.0', content-revision: 'fedcba' },
  ]
}
`,
			action: pinActionAddedSupportedVersions,
		},
		{
			name: "empty supported-versions list",
			src: `stack: {
  library-path: 'github.com/cloudboss/cluster-deploy'
  supported-versions: []
}
`,
			want: `stack: {
  library-path: 'github.com/cloudboss/cluster-deploy'
  supported-versions: [
    { version: 'v0.3.0', content-revision: 'fedcba' },
  ]
}
`,
			action: pinActionAppendedEntry,
		},
		{
			name: "supported-versions with existing entries, no trailing comma",
			src: `stack: {
  library-path: 'github.com/cloudboss/cluster-deploy'
  supported-versions: [
    { version: 'v0.1.0', content-revision: 'aaa' },
    { version: 'v0.2.0', content-revision: 'bbb' }
  ]
}
`,
			want: `stack: {
  library-path: 'github.com/cloudboss/cluster-deploy'
  supported-versions: [
    { version: 'v0.1.0', content-revision: 'aaa' },
    { version: 'v0.2.0', content-revision: 'bbb' },
    { version: 'v0.3.0', content-revision: 'fedcba' },
  ]
}
`,
			action: pinActionAppendedEntry,
		},
		{
			name: "supported-versions with existing entries, with trailing comma",
			src: `stack: {
  library-path: 'github.com/cloudboss/cluster-deploy'
  supported-versions: [
    { version: 'v0.1.0', content-revision: 'aaa' },
    { version: 'v0.2.0', content-revision: 'bbb' },
  ]
}
`,
			want: `stack: {
  library-path: 'github.com/cloudboss/cluster-deploy'
  supported-versions: [
    { version: 'v0.1.0', content-revision: 'aaa' },
    { version: 'v0.2.0', content-revision: 'bbb' },
    { version: 'v0.3.0', content-revision: 'fedcba' },
  ]
}
`,
			action: pinActionAppendedEntry,
		},
		{
			name: "idempotent when entry already present",
			src: `stack: {
  library-path: 'github.com/cloudboss/cluster-deploy'
  supported-versions: [
    { version: 'v0.3.0', content-revision: 'fedcba' },
  ]
}
`,
			want: `stack: {
  library-path: 'github.com/cloudboss/cluster-deploy'
  supported-versions: [
    { version: 'v0.3.0', content-revision: 'fedcba' },
  ]
}
`,
			action: pinActionAlreadyPinned,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, action, err := pinFile([]byte(tt.src), libraryPath, version, revision)
			require.NoError(t, err)
			assert.Equal(t, tt.action, action)
			assert.Equal(t, tt.want, string(got))
			_, err = lang.ParseSource("config.ub", got)
			require.NoError(t, err, "pinFile output failed to parse")
		})
	}
}

func TestPinFileRejectsLibraryPathMismatch(t *testing.T) {
	src := []byte(`stack: {
  library-path: 'github.com/cloudboss/other'
  supported-versions: []
}
`)
	_, _, err := pinFile(src, "github.com/cloudboss/cluster-deploy", "v0.1.0", "aaa")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "library-path")
}

func TestPinFilePreservesTrailingContent(t *testing.T) {
	src := []byte(`stack: {
  library-path: 'github.com/cloudboss/cluster-deploy'
  supported-versions: [
    { version: 'v0.1.0', content-revision: 'aaa' }
  ]
}

inputs: {
  message: 'hi'
}
`)
	want := `stack: {
  library-path: 'github.com/cloudboss/cluster-deploy'
  supported-versions: [
    { version: 'v0.1.0', content-revision: 'aaa' },
    { version: 'v0.3.0', content-revision: 'fedcba' },
  ]
}

inputs: {
  message: 'hi'
}
`
	got, action, err := pinFile(src, "github.com/cloudboss/cluster-deploy", "v0.3.0", "fedcba")
	require.NoError(t, err)
	assert.Equal(t, pinActionAppendedEntry, action)
	assert.Equal(t, want, string(got))
}
