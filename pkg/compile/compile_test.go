package compile

import (
	"testing"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/stretchr/testify/require"
)

func TestParseFactorySourceAcceptsSourceDeclaredFactory(t *testing.T) {
	src := []byte(`factory: {
  imports: { std: 'github.com/example/std' }
  inputs: { path: { type: string } }
  resources: {
    hello: std.fs-file { path: var.path }
  }
  outputs: {
    path: { value: resource.std.fs-file.hello.path }
  }
}
`)

	f, body, err := parseFactorySource("factory.ub", src)
	require.NoError(t, err)
	require.Equal(t, lang.FileFactory, f.Kind)
	require.NotContains(t, body, "factory:")
	require.Contains(t, body, "imports:")
	require.Contains(t, body, "std.fs-file.hello:")
	require.NoError(t, lang.ValidateFile(f).Err())
}

func TestDecideSelectedUnobin(t *testing.T) {
	tests := []struct {
		name       string
		listOutput string
		expected   string
		wantNotice string
		wantErr    string
	}{
		{
			name:       "selected equals expected",
			listOutput: "v0.1.0\n",
			expected:   "v0.1.0",
		},
		{
			name:       "replaced module proceeds with a notice",
			listOutput: "v0.0.0 replaced\n",
			expected:   "v0.0.0",
			wantNotice: "replaced",
		},
		{
			name:       "replaced module proceeds even when the version differs",
			listOutput: "v0.2.0 replaced\n",
			expected:   "v0.1.0",
			wantNotice: "replaced",
		},
		{
			name:       "newer selection without replace is refused",
			listOutput: "v0.2.0\n",
			expected:   "v0.1.0",
			wantErr:    "upgrade unobin to v0.2.0",
		},
		{
			name:       "unreadable output is refused",
			listOutput: "",
			expected:   "v0.1.0",
			wantErr:    "selected version",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			notice, err := decideSelectedUnobin(tt.listOutput, tt.expected)
			if tt.wantErr != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			if tt.wantNotice == "" {
				require.Empty(t, notice)
			} else {
				require.Contains(t, notice, tt.wantNotice)
			}
		})
	}
}
