package local

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	ufs "github.com/cloudboss/unobin/pkg/fs"
	"github.com/cloudboss/unobin/pkg/runtime"
)

// File writes a regular file to the local filesystem. The file's path
// is part of the input set; changing the path replaces the resource
// (the prior file is deleted and a new one is written at the new path).
// CreateDirectory opts the resource into creating any missing parent
// directories of Path. Without it, a missing parent is an error so
// callers do not accidentally write outside an expected tree.
type File struct {
	Path            string
	Content         string
	Mode            int64
	CreateDirectory bool
}

// FileOutput is what gets stored in state after Create / Update. It
// holds only what writing the file computes; path is an input and is
// readable as one, so it is not copied here.
type FileOutput struct {
	SHA256 string
	Size   int64
}

func (f *File) SchemaVersion() int      { return 1 }
func (f *File) ReplaceFields() []string { return []string{"path"} }

func (f *File) Create(_ context.Context, _ any) (*FileOutput, error) {
	return f.write()
}

func (f *File) Read(_ context.Context, _ any, _ *FileOutput) (*FileOutput, error) {
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
	return &FileOutput{
		SHA256: hex.EncodeToString(sum[:]),
		Size:   info.Size(),
	}, nil
}

func (f *File) Update(
	_ context.Context, _ any, _ runtime.Prior[File, *FileOutput],
) (*FileOutput, error) {
	return f.write()
}

func (f *File) Delete(_ context.Context, _ any, _ *FileOutput) error {
	err := os.Remove(f.Path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

func (f *File) write() (*FileOutput, error) {
	if f.Path == "" {
		return nil, errors.New("local.file: path is required")
	}
	mode := os.FileMode(f.Mode)
	if mode == 0 {
		mode = 0o644
	}
	if f.CreateDirectory {
		if err := os.MkdirAll(filepath.Dir(f.Path), 0o755); err != nil {
			return nil, err
		}
	}
	body := []byte(f.Content)
	if err := ufs.WriteFileAtomic(f.Path, body, mode); err != nil {
		return nil, err
	}
	sum := sha256.Sum256(body)
	return &FileOutput{
		SHA256: hex.EncodeToString(sum[:]),
		Size:   int64(len(body)),
	}, nil
}
