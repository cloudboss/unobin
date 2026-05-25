package runner

import (
	"testing"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPinFile(t *testing.T) {
	const (
		modulePath = "github.com/cloudboss/cluster-deploy"
		version    = "v0.3.0"
		commit     = "fedcba"
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
  module-path: 'github.com/cloudboss/cluster-deploy'
  supported-versions: [
    { version: 'v0.3.0', commit: 'fedcba' },
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
  module-path: 'github.com/cloudboss/cluster-deploy'
  supported-versions: [
    { version: 'v0.3.0', commit: 'fedcba' },
  ]
}
`,
			action: pinActionAddedStackBlock,
		},
		{
			name: "stack block missing supported-versions",
			src: `stack: {
  module-path: 'github.com/cloudboss/cluster-deploy'
}
`,
			want: `stack: {
  module-path: 'github.com/cloudboss/cluster-deploy'
  supported-versions: [
    { version: 'v0.3.0', commit: 'fedcba' },
  ]
}
`,
			action: pinActionAddedSupportedVersions,
		},
		{
			name: "stack block missing module-path and supported-versions",
			src: `stack: {
}
`,
			want: `stack: {
  module-path: 'github.com/cloudboss/cluster-deploy'
  supported-versions: [
    { version: 'v0.3.0', commit: 'fedcba' },
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
  module-path: 'github.com/cloudboss/cluster-deploy'
  supported-versions: [
    { version: 'v0.3.0', commit: 'fedcba' },
  ]
}
`,
			action: pinActionAddedSupportedVersions,
		},
		{
			name: "empty supported-versions list",
			src: `stack: {
  module-path: 'github.com/cloudboss/cluster-deploy'
  supported-versions: []
}
`,
			want: `stack: {
  module-path: 'github.com/cloudboss/cluster-deploy'
  supported-versions: [
    { version: 'v0.3.0', commit: 'fedcba' },
  ]
}
`,
			action: pinActionAppendedEntry,
		},
		{
			name: "supported-versions with existing entries, no trailing comma",
			src: `stack: {
  module-path: 'github.com/cloudboss/cluster-deploy'
  supported-versions: [
    { version: 'v0.1.0', commit: 'aaa' },
    { version: 'v0.2.0', commit: 'bbb' }
  ]
}
`,
			want: `stack: {
  module-path: 'github.com/cloudboss/cluster-deploy'
  supported-versions: [
    { version: 'v0.1.0', commit: 'aaa' },
    { version: 'v0.2.0', commit: 'bbb' },
    { version: 'v0.3.0', commit: 'fedcba' },
  ]
}
`,
			action: pinActionAppendedEntry,
		},
		{
			name: "supported-versions with existing entries, with trailing comma",
			src: `stack: {
  module-path: 'github.com/cloudboss/cluster-deploy'
  supported-versions: [
    { version: 'v0.1.0', commit: 'aaa' },
    { version: 'v0.2.0', commit: 'bbb' },
  ]
}
`,
			want: `stack: {
  module-path: 'github.com/cloudboss/cluster-deploy'
  supported-versions: [
    { version: 'v0.1.0', commit: 'aaa' },
    { version: 'v0.2.0', commit: 'bbb' },
    { version: 'v0.3.0', commit: 'fedcba' },
  ]
}
`,
			action: pinActionAppendedEntry,
		},
		{
			name: "idempotent when entry already present",
			src: `stack: {
  module-path: 'github.com/cloudboss/cluster-deploy'
  supported-versions: [
    { version: 'v0.3.0', commit: 'fedcba' },
  ]
}
`,
			want: `stack: {
  module-path: 'github.com/cloudboss/cluster-deploy'
  supported-versions: [
    { version: 'v0.3.0', commit: 'fedcba' },
  ]
}
`,
			action: pinActionAlreadyPinned,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, action, err := pinFile([]byte(tt.src), modulePath, version, commit)
			require.NoError(t, err)
			assert.Equal(t, tt.action, action)
			assert.Equal(t, tt.want, string(got))
			_, err = lang.ParseSource("config.ub", got)
			require.NoError(t, err, "pinFile output failed to parse")
		})
	}
}

func TestPinFileRejectsModulePathMismatch(t *testing.T) {
	src := []byte(`stack: {
  module-path: 'github.com/cloudboss/other'
  supported-versions: []
}
`)
	_, _, err := pinFile(src, "github.com/cloudboss/cluster-deploy", "v0.1.0", "aaa")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "module-path")
}

func TestPinFilePreservesTrailingContent(t *testing.T) {
	src := []byte(`stack: {
  module-path: 'github.com/cloudboss/cluster-deploy'
  supported-versions: [
    { version: 'v0.1.0', commit: 'aaa' }
  ]
}

inputs: {
  message: 'hi'
}
`)
	want := `stack: {
  module-path: 'github.com/cloudboss/cluster-deploy'
  supported-versions: [
    { version: 'v0.1.0', commit: 'aaa' },
    { version: 'v0.3.0', commit: 'fedcba' },
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
