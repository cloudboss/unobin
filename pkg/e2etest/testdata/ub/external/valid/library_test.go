package consumer_test

import (
	"testing"

	"github.com/cloudboss/unobin/pkg/e2etest"
)

func TestLibrary(t *testing.T) {
	e2etest.RunCompiledCases(t, "testdata/ub/valid",
		e2etest.WithGoModule("example.com/e2econsumer", "."),
		e2etest.WithEnv(map[string]string{"UB_INPUT_text": "external library"}),
	)
}
