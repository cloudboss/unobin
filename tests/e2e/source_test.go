package e2e

import (
	"testing"

	"github.com/cloudboss/unobin/internal/e2etest"
)

func TestSourceCases(t *testing.T) {
	e2etest.RunSourceCases(t, "testdata/source-cases")
}
