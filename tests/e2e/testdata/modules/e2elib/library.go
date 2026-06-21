package e2elib

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudboss/unobin/pkg/constraint"
	"github.com/cloudboss/unobin/pkg/defaults"
	ubruntime "github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
)

type Configuration struct {
	BaseDir      cfg.String `ub:"base-dir"`
	EventLogPath cfg.String `ub:"event-log-path"`
	Prefix       *cfg.String
	Nested       *NestedConfig
}

type NestedConfig struct {
	Label   cfg.String
	Enabled *cfg.Boolean
}

func Library() *ubruntime.Library {
	return &ubruntime.Library{
		Name:        "e2elib",
		Description: "Fixture library for Unobin e2e tests.",
		Configuration: &cfg.ConfigurationType[*Configuration]{
			Description: "Filesystem-backed e2e test settings.",
			New: func() *Configuration {
				return &Configuration{
					BaseDir:      cfg.String{Default: "."},
					EventLogPath: cfg.String{Default: "events.ndjson"},
					Prefix:       &cfg.String{Default: ""},
					Nested: &NestedConfig{
						Label:   cfg.String{Default: "nested"},
						Enabled: &cfg.Boolean{Default: true},
					},
				}
			},
		},
		Resources: map[string]ubruntime.ResourceRegistration{
			"file":   ubruntime.MakeResource[File, *FileOutput, *Configuration](),
			"object": ubruntime.MakeResource[Object, *ObjectOutput, *Configuration](),
		},
		DataSources: map[string]ubruntime.DataSourceRegistration{
			"read-file": ubruntime.MakeDataSource[ReadFile, *ReadFileOutput, *Configuration](),
		},
		Actions: map[string]ubruntime.ActionRegistration{
			"echo":   ubruntime.MakeAction[Echo, *EchoOutput, *Configuration](),
			"record": ubruntime.MakeAction[Record, *RecordOutput, *Configuration](),
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

type File struct {
	Path          string
	Content       string
	Mode          int64
	CreateParents bool  `ub:"create-parents"`
	FailUpdate    *bool `ub:"fail-update"`
	Tags          map[string]string
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
		defaults.Optional(f.Tags),
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
	Name string
	Body map[string]any
	Tags map[string]string
}

type ObjectOutput struct {
	ID     string
	Path   string
	Body   map[string]any
	SHA256 string
}

func (o Object) Defaults() []defaults.Default {
	return []defaults.Default{defaults.Optional(o.Tags)}
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
	Tags    map[string]string
}

type RecordOutput struct {
	Record string
	Name   string
	Count  int64
}

func (r Record) Defaults() []defaults.Default {
	return []defaults.Default{defaults.Optional(r.Tags)}
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
	if config == nil || config.BaseDir.Value == "" {
		return "."
	}
	return config.BaseDir.Value
}

func configPrefix(config *Configuration) string {
	if config == nil || config.Prefix == nil {
		return ""
	}
	return config.Prefix.Value
}

func eventLogPath(config *Configuration) string {
	path := "events.ndjson"
	if config != nil && config.EventLogPath.Value != "" {
		path = config.EventLogPath.Value
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
