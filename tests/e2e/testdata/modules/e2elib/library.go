package e2elib

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	pathpkg "path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/defaults"
	ubruntime "github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
)

type Configuration struct {
	BaseDir      string `ub:"base-dir"`
	EventLogPath string `ub:"event-log-path"`
	Prefix       string
	Nested       NestedConfig
}

type NestedConfig struct {
	Label   string
	Enabled bool
}

func (c Configuration) Defaults() []defaults.Default {
	return []defaults.Default{
		defaults.Value(c.BaseDir, "."),
		defaults.Value(c.EventLogPath, "events.ndjson"),
		defaults.Value(c.Prefix, ""),
		defaults.Value(c.Nested.Label, "nested"),
		defaults.Value(c.Nested.Enabled, true),
	}
}

func Library() *ubruntime.Library {
	return &ubruntime.Library{
		Name:        "e2elib",
		Description: "Fixture library for Unobin e2e tests.",
		Configuration: &cfg.ConfigurationType[*Configuration]{
			Description: "Filesystem-backed e2e test settings.",
			New:         func() *Configuration { return &Configuration{} },
		},
		Resources: map[string]ubruntime.ResourceRegistration{
			"archive-zipfile": ubruntime.MakeResource[
				ArchiveZIPFile,
				*ArchiveZIPFileOutput,
				*Configuration,
			](),
			"file":   ubruntime.MakeResource[File, *FileOutput, *Configuration](),
			"object": ubruntime.MakeResource[Object, *ObjectOutput, *Configuration](),
		},
		DataSources: map[string]ubruntime.DataSourceRegistration{
			"read-file": ubruntime.MakeDataSource[ReadFile, *ReadFileOutput, *Configuration](),
		},
		Actions: map[string]ubruntime.ActionRegistration{
			"echo":   ubruntime.MakeAction[Echo, *EchoOutput, *Configuration](),
			"record": ubruntime.MakeAction[Record, *RecordOutput, *Configuration](),
			"secret": ubruntime.MakeAction[Secret, *SecretOutput, *Configuration](),
		},
		Functions: map[string]ubruntime.FunctionType{
			"all": ubruntime.MakeFunc("all", "Return true when every argument is true.", fnAll),
			"all-list": ubruntime.MakeFunc(
				"all-list",
				"Return true when every list item is true.",
				fnAllList,
			),
			"join":    ubruntime.MakeFunc("join", "Join strings with a separator.", fnJoin),
			"length":  ubruntime.MakeFunc("length", "Return a value length.", fnLength),
			"project": ubruntime.MakeFunc("project", "Read an object field.", fnProject),
			"fail":    ubruntime.MakeFunc("fail", "Return a typed error.", fnFail),
		},
	}
}

type ArchiveZIPFile struct {
	Path                    string
	SourceDir               string            `ub:"source-dir"`
	SelectedPath            string            `ub:"selected-path"`
	FileContent             []byte            `ub:"file-content"`
	ArchiveContent          []byte            `ub:"archive-content"`
	ExpectedFileSHA256      string            `ub:"expected-file-sha256"`
	ExpectedFileMode        string            `ub:"expected-file-mode"`
	ExpectedDirectorySHA256 string            `ub:"expected-directory-sha256"`
	ExpectedDirectoryMode   string            `ub:"expected-directory-mode"`
	ExpectedEntries         map[string]string `ub:"expected-entries"`
}

type ArchiveZIPFileOutput struct {
	Path          string
	FileSHA256    string `ub:"file-sha256"`
	FileMode      string `ub:"file-mode"`
	ArchiveSHA256 string `ub:"archive-sha256"`
	DirectoryMode string `ub:"directory-mode"`
	Entries       map[string]string
}

func (a ArchiveZIPFile) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.NotEmpty(a.Path)).Message("path is required"),
		constraint.Must(constraint.NotEmpty(a.SourceDir)).Message("source-dir is required"),
		constraint.Must(constraint.NotEmpty(a.SelectedPath)).Message("selected-path is required"),
		constraint.Must(constraint.NotEmpty(a.ExpectedFileSHA256)).
			Message("expected-file-sha256 is required"),
		constraint.Must(constraint.NotEmpty(a.ExpectedFileMode)).
			Message("expected-file-mode is required"),
		constraint.Must(constraint.NotEmpty(a.ExpectedDirectorySHA256)).
			Message("expected-directory-sha256 is required"),
		constraint.Must(constraint.NotEmpty(a.ExpectedDirectoryMode)).
			Message("expected-directory-mode is required"),
	}
}

func (a *ArchiveZIPFile) SchemaVersion() int { return 1 }

func (a *ArchiveZIPFile) ReplaceFields() []string { return []string{"path"} }

func (a *ArchiveZIPFile) Create(
	_ context.Context,
	config *Configuration,
) (*ArchiveZIPFileOutput, error) {
	return a.write(config)
}

func (a *ArchiveZIPFile) Read(
	_ context.Context,
	config *Configuration,
	_ *ArchiveZIPFileOutput,
) (*ArchiveZIPFileOutput, error) {
	path := resolvePath(config, a.Path)
	content, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, ubruntime.ErrNotFound
		}
		return nil, err
	}
	if !bytes.Equal(content, a.ArchiveContent) {
		return nil, fmt.Errorf("archive file content differs from archive-content")
	}
	return a.verify()
}

func (a *ArchiveZIPFile) Update(
	_ context.Context,
	config *Configuration,
	_ ubruntime.Prior[ArchiveZIPFile, *ArchiveZIPFileOutput],
) (*ArchiveZIPFileOutput, error) {
	return a.write(config)
}

func (a *ArchiveZIPFile) Delete(
	_ context.Context,
	config *Configuration,
	_ *ArchiveZIPFileOutput,
) error {
	err := os.Remove(resolvePath(config, a.Path))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

func (a *ArchiveZIPFile) write(config *Configuration) (*ArchiveZIPFileOutput, error) {
	output, err := a.verify()
	if err != nil {
		return nil, err
	}
	path := resolvePath(config, a.Path)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, a.ArchiveContent, 0o644); err != nil {
		return nil, err
	}
	return output, nil
}

func (a *ArchiveZIPFile) verify() (*ArchiveZIPFileOutput, error) {
	sourceMode, entries, err := inspectFixtureDirectory(a.SourceDir)
	if err != nil {
		return nil, err
	}
	if sourceMode != a.ExpectedDirectoryMode {
		return nil, fmt.Errorf(
			"source directory mode is %s, want %s",
			sourceMode,
			a.ExpectedDirectoryMode,
		)
	}
	entryModes := make(map[string]string, len(entries))
	for name, entry := range entries {
		entryModes[name] = entry.mode
	}
	if !maps.Equal(entryModes, a.ExpectedEntries) {
		return nil, fmt.Errorf(
			"source entries are %v, want %v",
			sortedEntryModes(entryModes),
			sortedEntryModes(a.ExpectedEntries),
		)
	}

	selectedInfo, err := os.Lstat(a.SelectedPath)
	if err != nil {
		return nil, fmt.Errorf("inspect selected path: %w", err)
	}
	selectedMode, err := fixtureMode(selectedInfo.Mode())
	if err != nil || !selectedInfo.Mode().IsRegular() {
		return nil, fmt.Errorf("selected path is not a regular file")
	}
	selectedContent, err := os.ReadFile(a.SelectedPath)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(selectedContent, a.FileContent) {
		return nil, fmt.Errorf("selected file content differs from file-content")
	}
	fileSHA256 := hashBytes(selectedContent)
	if fileSHA256 != a.ExpectedFileSHA256 {
		return nil, fmt.Errorf(
			"selected file SHA-256 is %s, want %s",
			fileSHA256,
			a.ExpectedFileSHA256,
		)
	}
	if selectedMode != a.ExpectedFileMode {
		return nil, fmt.Errorf(
			"selected file mode is %s, want %s",
			selectedMode,
			a.ExpectedFileMode,
		)
	}
	foundSelected := false
	for _, entry := range entries {
		if !entry.directory &&
			entry.mode == selectedMode &&
			bytes.Equal(entry.content, selectedContent) {
			foundSelected = true
			break
		}
	}
	if !foundSelected {
		return nil, fmt.Errorf("selected file is not present in source-dir")
	}

	archiveSHA256 := hashBytes(a.ArchiveContent)
	if archiveSHA256 != a.ExpectedDirectorySHA256 {
		return nil, fmt.Errorf(
			"archive SHA-256 is %s, want %s",
			archiveSHA256,
			a.ExpectedDirectorySHA256,
		)
	}
	archiveEntries, err := inspectFixtureArchive(a.ArchiveContent)
	if err != nil {
		return nil, err
	}
	if err := compareFixtureEntries(entries, archiveEntries); err != nil {
		return nil, err
	}
	return &ArchiveZIPFileOutput{
		Path:          a.Path,
		FileSHA256:    fileSHA256,
		FileMode:      selectedMode,
		ArchiveSHA256: archiveSHA256,
		DirectoryMode: sourceMode,
		Entries:       maps.Clone(entryModes),
	}, nil
}

type fixtureEntry struct {
	mode      string
	content   []byte
	directory bool
}

func inspectFixtureDirectory(root string) (string, map[string]fixtureEntry, error) {
	rootInfo, err := os.Lstat(root)
	if err != nil {
		return "", nil, err
	}
	rootMode, err := fixtureMode(rootInfo.Mode())
	if err != nil || !rootInfo.IsDir() {
		return "", nil, fmt.Errorf("source-dir is not a directory")
	}
	entries := map[string]fixtureEntry{}
	err = filepath.WalkDir(root, func(path string, item fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, path)
		if err != nil || relative == "." {
			return err
		}
		info, err := item.Info()
		if err != nil {
			return err
		}
		mode, err := fixtureMode(info.Mode())
		if err != nil {
			return fmt.Errorf("source entry %s: %w", relative, err)
		}
		entry := fixtureEntry{mode: mode, directory: item.IsDir()}
		if !item.IsDir() {
			entry.content, err = os.ReadFile(path)
			if err != nil {
				return err
			}
		}
		entries[filepath.ToSlash(relative)] = entry
		return nil
	})
	if err != nil {
		return "", nil, err
	}
	return rootMode, entries, nil
}

func inspectFixtureArchive(content []byte) (map[string]fixtureEntry, error) {
	reader, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		return nil, fmt.Errorf("open archive-content: %w", err)
	}
	entries := make(map[string]fixtureEntry, len(reader.File))
	for _, file := range reader.File {
		directory := strings.HasSuffix(file.Name, "/")
		name := strings.TrimSuffix(file.Name, "/")
		if name == "" ||
			name != pathpkg.Clean(name) ||
			!fs.ValidPath(name) ||
			strings.Contains(name, `\`) {
			return nil, fmt.Errorf("archive-content has invalid entry %q", file.Name)
		}
		if _, ok := entries[name]; ok {
			return nil, fmt.Errorf("archive-content has duplicate entry %q", name)
		}
		mode, err := fixtureMode(file.Mode())
		if err != nil {
			return nil, fmt.Errorf("archive entry %s: %w", name, err)
		}
		if directory != file.FileInfo().IsDir() {
			return nil, fmt.Errorf("archive entry %s has inconsistent directory mode", name)
		}
		entry := fixtureEntry{mode: mode, directory: directory}
		if !directory {
			stream, err := file.Open()
			if err != nil {
				return nil, err
			}
			entry.content, err = io.ReadAll(stream)
			closeErr := stream.Close()
			if err != nil {
				return nil, err
			}
			if closeErr != nil {
				return nil, closeErr
			}
		}
		entries[name] = entry
	}
	return entries, nil
}

func compareFixtureEntries(
	source map[string]fixtureEntry,
	archive map[string]fixtureEntry,
) error {
	if !maps.EqualFunc(source, archive, func(a, b fixtureEntry) bool {
		return a.mode == b.mode &&
			a.directory == b.directory &&
			bytes.Equal(a.content, b.content)
	}) {
		return fmt.Errorf("archive-content entries differ from source-dir")
	}
	return nil
}

func fixtureMode(mode fs.FileMode) (string, error) {
	switch {
	case mode.IsDir():
		return "0755", nil
	case mode.IsRegular() && mode.Perm()&0o111 != 0:
		return "0755", nil
	case mode.IsRegular():
		return "0644", nil
	default:
		return "", fmt.Errorf("unsupported mode %s", mode)
	}
}

func sortedEntryModes(entries map[string]string) []string {
	out := make([]string, 0, len(entries))
	for name, mode := range entries {
		out = append(out, name+"="+mode)
	}
	slices.Sort(out)
	return out
}

type File struct {
	Path          string
	Content       string
	Mode          int64
	CreateParents bool  `ub:"create-parents"`
	FailUpdate    *bool `ub:"fail-update"`
	Tags          *map[string]string
}

type FileOutput struct {
	Path    string
	Content string
	Size    int64
	SHA256  string
	Exists  bool
}

func (f File) Defaults() []defaults.Default {
	return []defaults.Default{
		defaults.Value(f.Mode, 420),
		defaults.Value(f.CreateParents, true),
	}
}

func (f File) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.NotEmpty(f.Path)).Message("path is required"),
		constraint.Must(constraint.AtLeast(f.Mode, 0)).
			Message("mode must be non-negative"),
	}
}

func (f *File) SchemaVersion() int { return 1 }

func (f *File) ReplaceFields() []string { return []string{"path"} }

func (f *File) Create(_ context.Context, config *Configuration) (*FileOutput, error) {
	return f.write(config, "create")
}

func (f *File) Read(
	_ context.Context,
	config *Configuration,
	prior *FileOutput,
) (*FileOutput, error) {
	path := resolvePath(config, f.Path)
	if prior != nil && prior.Path != "" {
		path = prior.Path
	}
	body, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, ubruntime.ErrNotFound
		}
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	return fileOutput(path, body, info.Size()), nil
}

func (f *File) Update(
	_ context.Context,
	config *Configuration,
	_ ubruntime.Prior[File, *FileOutput],
) (*FileOutput, error) {
	if f.FailUpdate != nil && *f.FailUpdate {
		return nil, errors.New("file update failed")
	}
	return f.write(config, "update")
}

func (f *File) Delete(_ context.Context, config *Configuration, prior *FileOutput) error {
	path := resolvePath(config, f.Path)
	if prior != nil && prior.Path != "" {
		path = prior.Path
	}
	err := os.Remove(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return appendEvent(config, event{Operation: "delete", Kind: "file", Path: path})
}

func (f *File) write(config *Configuration, operation string) (*FileOutput, error) {
	path := resolvePath(config, f.Path)
	if f.CreateParents {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, err
		}
	}
	mode := fs.FileMode(f.Mode)
	if mode == 0 {
		mode = 0o644
	}
	body := []byte(f.Content)
	if err := os.WriteFile(path, body, mode.Perm()); err != nil {
		return nil, err
	}
	out := fileOutput(path, body, int64(len(body)))
	err := appendEvent(config, event{
		Operation: operation,
		Kind:      "file",
		Path:      path,
		SHA256:    out.SHA256,
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

type Object struct {
	Name      string
	Body      map[string]any
	Tags      *map[string]string
	SubnetIDs *[]string `ub:"subnet-ids"`
}

type ObjectOutput struct {
	ID     string
	Path   string
	Body   map[string]any
	SHA256 string
}

func (o Object) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.NotEmpty(o.Name)).Message("name is required"),
	}
}

func (o *Object) SchemaVersion() int { return 1 }

func (o *Object) ReplaceFields() []string { return []string{"name"} }

func (o *Object) Create(_ context.Context, config *Configuration) (*ObjectOutput, error) {
	return o.write(config, "create")
}

func (o *Object) Read(
	_ context.Context,
	config *Configuration,
	prior *ObjectOutput,
) (*ObjectOutput, error) {
	path := objectPath(config, o.Name)
	if prior != nil && prior.Path != "" {
		path = prior.Path
	}
	body, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, ubruntime.ErrNotFound
		}
		return nil, err
	}
	var value map[string]any
	if err := json.Unmarshal(body, &value); err != nil {
		return nil, err
	}
	out := objectOutput(config, o.Name, value, body)
	out.Path = path
	if prior != nil && prior.ID != "" {
		out.ID = prior.ID
	}
	return out, nil
}

func (o *Object) Update(
	_ context.Context,
	config *Configuration,
	_ ubruntime.Prior[Object, *ObjectOutput],
) (*ObjectOutput, error) {
	return o.write(config, "update")
}

func (o *Object) Delete(_ context.Context, config *Configuration, prior *ObjectOutput) error {
	path := objectPath(config, o.Name)
	if prior != nil && prior.Path != "" {
		path = prior.Path
	}
	err := os.Remove(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return appendEvent(config, event{Operation: "delete", Kind: "object", Path: path})
}

func (o *Object) write(config *Configuration, operation string) (*ObjectOutput, error) {
	path := objectPath(config, o.Name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	body, err := json.MarshalIndent(o.Body, "", "  ")
	if err != nil {
		return nil, err
	}
	body = append(body, '\n')
	if err := os.WriteFile(path, body, 0o644); err != nil {
		return nil, err
	}
	out := objectOutput(config, o.Name, o.Body, body)
	err = appendEvent(config, event{
		Operation: operation,
		Kind:      "object",
		Name:      o.Name,
		Path:      path,
		SHA256:    out.SHA256,
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

type ReadFile struct {
	Path     string
	Optional bool
}

type ReadFileOutput struct {
	Path    string
	Content string
	Size    int64
	SHA256  string
	Exists  bool
}

func (r ReadFile) Defaults() []defaults.Default {
	return []defaults.Default{defaults.Value(r.Optional, false)}
}

func (r ReadFile) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.NotEmpty(r.Path)).Message("path is required"),
	}
}

func (r *ReadFile) Read(_ context.Context, config *Configuration) (*ReadFileOutput, error) {
	path := resolvePath(config, r.Path)
	body, err := os.ReadFile(path)
	if err != nil {
		if r.Optional && errors.Is(err, fs.ErrNotExist) {
			return &ReadFileOutput{Path: path, Exists: false}, nil
		}
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	out := fileOutput(path, body, info.Size())
	return &ReadFileOutput{
		Path:    out.Path,
		Content: out.Content,
		Size:    out.Size,
		SHA256:  out.SHA256,
		Exists:  out.Exists,
	}, nil
}

type Echo struct {
	Text  string
	Upper bool
}

type EchoOutput struct {
	Text     string
	Prefixed string
	Length   int64
}

func (e Echo) Defaults() []defaults.Default {
	return []defaults.Default{defaults.Value(e.Upper, false)}
}

func (e *Echo) Run(_ context.Context, config *Configuration) (*EchoOutput, error) {
	text := e.Text
	if e.Upper {
		text = strings.ToUpper(text)
	}
	return &EchoOutput{
		Text:     text,
		Prefixed: configPrefix(config) + text,
		Length:   int64(len(text)),
	}, nil
}

type Record struct {
	Name    string
	Message string
	Tags    *map[string]string
}

type RecordOutput struct {
	Record string
	Name   string
	Count  int64
}

type Secret struct {
	Label string
	Value string
}

type SecretOutput struct {
	Label string
	Value string `ub:",sensitive"`
}

func (r Record) Constraints() []constraint.Constraint {
	return []constraint.Constraint{
		constraint.Must(constraint.NotEmpty(r.Name)).Message("name is required"),
	}
}

func (r *Record) Run(_ context.Context, config *Configuration) (*RecordOutput, error) {
	record := configPrefix(config) + r.Name + ":" + r.Message
	err := appendEvent(config, event{
		Operation: "run",
		Kind:      "record",
		Name:      r.Name,
		Message:   r.Message,
	})
	if err != nil {
		return nil, err
	}
	return &RecordOutput{Record: record, Name: r.Name, Count: 1}, nil
}

func (s *Secret) Run(_ context.Context, _ *Configuration) (*SecretOutput, error) {
	return &SecretOutput{Label: s.Label, Value: s.Value}, nil
}

type event struct {
	Operation string `json:"operation"`
	Kind      string `json:"kind"`
	Name      string `json:"name,omitempty"`
	Path      string `json:"path,omitempty"`
	SHA256    string `json:"sha256,omitempty"`
	Message   string `json:"message,omitempty"`
}

type FunctionError struct {
	Message string
}

func (e *FunctionError) Error() string { return "e2elib: " + e.Message }

func fnAll(values ...bool) (bool, error) {
	for _, value := range values {
		if !value {
			return false, nil
		}
	}
	return true, nil
}

func fnAllList(values []bool) (bool, error) {
	for _, value := range values {
		if !value {
			return false, nil
		}
	}
	return true, nil
}

func fnJoin(sep string, values ...string) (string, error) {
	return strings.Join(values, sep), nil
}

func fnLength(value any) (int64, error) {
	switch v := value.(type) {
	case string:
		return int64(len(v)), nil
	case []any:
		return int64(len(v)), nil
	case []string:
		return int64(len(v)), nil
	case map[string]any:
		return int64(len(v)), nil
	case map[string]string:
		return int64(len(v)), nil
	default:
		return 0, fmt.Errorf("length: unsupported value %T", value)
	}
}

func fnProject(value map[string]any, key string) (any, error) {
	out, ok := value[key]
	if !ok {
		return nil, &FunctionError{Message: "missing key " + key}
	}
	return out, nil
}

func fnFail(message string) (string, error) {
	return "", &FunctionError{Message: message}
}

func resolvePath(config *Configuration, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(configBaseDir(config), path)
}

func objectPath(config *Configuration, name string) string {
	fileName := configPrefix(config) + name + ".json"
	return resolvePath(config, filepath.Join("objects", fileName))
}

func configBaseDir(config *Configuration) string {
	if config == nil || config.BaseDir == "" {
		return "."
	}
	return config.BaseDir
}

func configPrefix(config *Configuration) string {
	if config == nil {
		return ""
	}
	return config.Prefix
}

func eventLogPath(config *Configuration) string {
	path := "events.ndjson"
	if config != nil && config.EventLogPath != "" {
		path = config.EventLogPath
	}
	return resolvePath(config, path)
}

func appendEvent(config *Configuration, ev event) error {
	path := eventLogPath(config)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	return json.NewEncoder(file).Encode(ev)
}

func fileOutput(path string, body []byte, size int64) *FileOutput {
	return &FileOutput{
		Path:    path,
		Content: string(body),
		Size:    size,
		SHA256:  hashBytes(body),
		Exists:  true,
	}
}

func objectOutput(
	config *Configuration,
	name string,
	value map[string]any,
	body []byte,
) *ObjectOutput {
	return &ObjectOutput{
		ID:     configPrefix(config) + name,
		Path:   objectPath(config, name),
		Body:   value,
		SHA256: hashBytes(body),
	}
}

func hashBytes(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}
