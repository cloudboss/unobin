package consumer

import (
	"context"
	"os"

	"github.com/cloudboss/unobin/pkg/runtime"
)

func Library() *runtime.Library {
	return &runtime.Library{
		Name: "consumer",
		Actions: map[string]runtime.ActionRegistration{
			"write": runtime.MakeAction[Write, *WriteOutput, runtime.NoConfig](),
		},
	}
}

type Write struct {
	Text string
}

type WriteOutput struct {
	Text string
}

func (w *Write) Run(_ context.Context, _ runtime.NoConfig) (*WriteOutput, error) {
	if err := os.WriteFile("result.txt", []byte(w.Text+"\n"), 0o600); err != nil {
		return nil, err
	}
	return &WriteOutput{Text: w.Text}, nil
}
