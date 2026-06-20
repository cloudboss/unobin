package e2e

import (
	"testing"

	"github.com/cloudboss/unobin/internal/e2etest"
)

func TestCompiledCases(t *testing.T) {
	e2etest.RunCompiledCases(t, "testdata/compiled-cases")
}
