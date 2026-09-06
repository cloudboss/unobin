package e2e

import (
	"path/filepath"
	"testing"

	"github.com/cloudboss/unobin/pkg/e2etest"
)

func TestSourceCases(t *testing.T) {
	e2etest.RunSourceCases(t, "testdata/source-cases",
		e2etest.WithUnobinDir(filepath.Join("..", "..")),
		e2etest.WithSourceDirectory("modules/e2elib", "testdata/modules/e2elib"),
	)
}
