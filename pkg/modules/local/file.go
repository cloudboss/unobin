package local

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"

	ufs "github.com/cloudboss/unobin/pkg/fs"
	"github.com/cloudboss/unobin/pkg/runtime"
)

// File writes a regular file to the local filesystem. The file's path
// is part of the input set; changing the path replaces the resource
// (the prior file is deleted and a new one is written at the new path).
type File struct {
	Path    string `mapstructure:"path"`
	Content string `mapstructure:"content"`
	Mode    int64  `mapstructure:"mode"`
}

// FileOutputs is what gets stored in state after Create / Update.
type FileOutputs struct {
	Path   string `mapstructure:"path"`
	SHA256 string `mapstructure:"sha256"`
	Size   int64  `mapstructure:"size"`
}

func (f *File) ReplaceFields() []string { return []string{"path"} }

func (f *File) Create(_ context.Context) (any, error) {
	return f.write()
}

func (f *File) Read(_ context.Context, prior any) (any, error) {
	info, err := os.Stat(f.Path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, runtime.ErrNotFound
		}
		return nil, err
	}
	body, err := os.ReadFile(f.Path)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(body)
	return FileOutputs{
		Path:   f.Path,
		SHA256: hex.EncodeToString(sum[:]),
		Size:   info.Size(),
	}, nil
}

func (f *File) Update(_ context.Context, _ any) (any, error) {
	return f.write()
}

func (f *File) Delete(_ context.Context, _ any) error {
	err := os.Remove(f.Path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

func (f *File) write() (FileOutputs, error) {
	if f.Path == "" {
		return FileOutputs{}, errors.New("local.file: path is required")
	}
	mode := os.FileMode(f.Mode)
	if mode == 0 {
		mode = 0o644
	}
	body := []byte(f.Content)
	if err := ufs.WriteFileAtomic(f.Path, body, mode); err != nil {
		return FileOutputs{}, err
	}
	sum := sha256.Sum256(body)
	return FileOutputs{
		Path:   f.Path,
		SHA256: hex.EncodeToString(sum[:]),
		Size:   int64(len(body)),
	}, nil
}
