package library

import (
	"context"
	"os"

	"example.com/identity/helper"
	"github.com/cloudboss/unobin/pkg/runtime"
)

type Marker struct{ Path string }
type Output struct{ Path string }

func (m *Marker) Create(_ context.Context, _ *Configuration) (*Output, error) {
	if err := os.WriteFile(m.Path, []byte(helper.Message()+"-resource-changed"), 0o644); err != nil {
		return nil, err
	}
	return &Output{Path: m.Path}, nil
}

func (m *Marker) Read(
	_ context.Context, _ *Configuration, _ runtime.Prior[Marker, *Output, *Configuration],
) (*Output, error) {
	return nil, runtime.ErrNotFound
}

func (m *Marker) Update(
	ctx context.Context, config *Configuration, _ runtime.Prior[Marker, *Output, *Configuration],
) (*Output, error) {
	return m.Create(ctx, config)
}

func (m *Marker) Delete(
	_ context.Context, _ *Configuration, _ runtime.Prior[Marker, *Output, *Configuration],
) error {
	return os.Remove(m.Path)
}
