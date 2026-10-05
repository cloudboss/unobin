package runtime

import (
	"os"
	"testing"
)

func TestMutateSource(t *testing.T) {
	if err := os.WriteFile("work.go", []byte("package runtime\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Fatal("Intentional collection failure")
}
