package e2e

import (
	"path/filepath"
	"testing"

	"github.com/cloudboss/unobin/pkg/e2etest"
)

func TestCompiledCases(t *testing.T) {
	e2etest.RunCompiledCases(t, "testdata/compiled-cases",
		e2etest.WithUnobinDir(filepath.Join("..", "..")),
		e2etest.WithGoModule("example.com/unobin/e2elib", "testdata/modules/e2elib"),
	)
}
