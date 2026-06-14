package syntax

import (
	"strings"
	"testing"

	"github.com/cloudboss/unobin/pkg/ubtest"
)

func TestValidateFileFixtures(t *testing.T) {
	ubtest.Run(t, "testdata/ub/validate", func(name string, src []byte) (string, []string) {
		f, err := ParseSource(validateFixturePath(name), src)
		if err != nil {
			return "", []string{err.Error()}
		}
		return "", ValidateFile(f).Messages()
	})
}

func validateFixturePath(name string) string {
	parts := strings.Split(name, "/")
	if len(parts) < 2 {
		return name + ".ub"
	}
	switch parts[1] {
	case "factory":
		return "factory.ub"
	case "lock":
		return "lock.ub"
	case "manifest":
		return "manifest.ub"
	case "stack":
		return "dev.ub"
	default:
		return parts[len(parts)-1] + ".ub"
	}
}
