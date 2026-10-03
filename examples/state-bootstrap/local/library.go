package local

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"

	"github.com/cloudboss/unobin/pkg/fs"
	"github.com/cloudboss/unobin/pkg/runtime"
)

func Library() *runtime.Library {
	return &runtime.Library{
		Name: "local",
		Resources: map[string]runtime.ResourceRegistration{
			"file": runtime.MakeResource[File, *FileOutput, runtime.NoConfig](
				runtime.ResourceDefinition[File, *FileOutput, runtime.NoConfig]{
					SchemaVersion: 1,
					Replace: runtime.Replacement[File, *FileOutput, runtime.NoConfig]{
						Fields: []runtime.AnyInputField[File]{
							runtime.InputField(func(input *File) *string { return &input.Path }),
						},
					},
				},
			),
		},
	}
}

type File struct {
	Path    string
	Content string
}

type FileOutput struct {
	Size   int64
	SHA256 string
}

func (f *File) Create(_ context.Context, _ runtime.NoConfig) (*FileOutput, error) {
	return f.write()
}

func (f *File) Read(
	_ context.Context,
	_ runtime.NoConfig,
	prior runtime.Prior[File, *FileOutput, runtime.NoConfig],
) (*FileOutput, error) {
	body, err := os.ReadFile(prior.Inputs.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, runtime.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return fileOutput(body), nil
}

func (f *File) Update(
	_ context.Context,
	_ runtime.NoConfig,
	_ runtime.Prior[File, *FileOutput, runtime.NoConfig],
) (*FileOutput, error) {
	return f.write()
}

func (f *File) Delete(
	_ context.Context,
	_ runtime.NoConfig,
	prior runtime.Prior[File, *FileOutput, runtime.NoConfig],
) error {
	err := os.Remove(prior.Inputs.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (f *File) write() (*FileOutput, error) {
	if err := os.MkdirAll(filepath.Dir(f.Path), 0o755); err != nil {
		return nil, err
	}
	body := []byte(f.Content)
	if err := fs.WriteFileAtomic(f.Path, body, 0o644); err != nil {
		return nil, err
	}
	return fileOutput(body), nil
}

func fileOutput(body []byte) *FileOutput {
	sum := sha256.Sum256(body)
	return &FileOutput{Size: int64(len(body)), SHA256: hex.EncodeToString(sum[:])}
}
