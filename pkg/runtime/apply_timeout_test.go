package runtime

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
)

func TestExtractTimeout(t *testing.T) {
	tests := []struct {
		name    string
		fixture string
		want    time.Duration
	}{
		{name: "action", fixture: "action-timeout", want: 30 * time.Second},
		{name: "resource", fixture: "resource-timeout", want: 5 * time.Minute},
		{name: "data-source", fixture: "data-timeout", want: 90 * time.Minute},
		{name: "none", fixture: "no-timeout", want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := ubtest.ReadValidFixture(t, "testdata/ub/apply-timeout", tt.fixture)
			nodes := ExtractSyntaxNodes(syntaxFactoryBody(t, src), nil)
			require.Len(t, nodes, 1)
			assert.Equal(t, tt.want, nodes[0].Timeout)
		})
	}
}
