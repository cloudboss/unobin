package runtime

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
)

func TestExtractLockName(t *testing.T) {
	tests := []struct {
		name    string
		fixture string
		want    string
	}{
		{name: "action", fixture: "extract-action", want: "kubectl"},
		{name: "resource", fixture: "extract-resource", want: "sg"},
		{name: "data-source", fixture: "extract-data", want: "reads"},
		{name: "no lock", fixture: "extract-none", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := ubtest.ReadValidFixture(t, "testdata/ub/apply-lock", tt.fixture)
			nodes := ExtractSyntaxNodes(syntaxFactoryBody(t, src), nil)
			require.Len(t, nodes, 1)
			assert.Equal(t, tt.want, nodes[0].LockName)
		})
	}
}
