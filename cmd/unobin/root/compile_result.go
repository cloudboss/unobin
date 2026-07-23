package root

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/cloudboss/unobin/internal/cmdout"
	compilepkg "github.com/cloudboss/unobin/pkg/compile"
	"github.com/cloudboss/unobin/pkg/diagnostic"
	"github.com/cloudboss/unobin/pkg/filechange"
	"github.com/spf13/cobra"
)

const maxCompileToolOutput = 1 << 20

type compileFactoryResult struct {
	Name            string  `json:"name"             ub:"name"`
	Version         string  `json:"version"          ub:"version"`
	ContentRevision *string `json:"content-revision" ub:"content-revision"`
	LibraryPath     *string `json:"library-path"     ub:"library-path"`
}

type compileSourceResult struct {
	Path       string `json:"path"        ub:"path"`
	ProjectDir string `json:"project-dir" ub:"project-dir"`
}

type compileOutputResult struct {
	Dir    string  `json:"dir"     ub:"dir"`
	MainGo string  `json:"main-go" ub:"main-go"`
	GoMod  string  `json:"go-mod"  ub:"go-mod"`
	Assets *string `json:"assets,omitempty" ub:"assets,omitempty"`
	Built  bool    `json:"built"   ub:"built"`
	Binary *string `json:"binary"  ub:"binary"`
}

type compileCommandResult struct {
	Kind          string                  `json:"kind"           ub:"kind"`
	FormatVersion int                     `json:"format-version" ub:"format-version"`
	Factory       compileFactoryResult    `json:"factory"        ub:"factory"`
	Source        compileSourceResult     `json:"source"         ub:"source"`
	Output        compileOutputResult     `json:"output"         ub:"output"`
	Files         []filechange.Change     `json:"files"          ub:"files"`
	Diagnostics   []diagnostic.Diagnostic `json:"diagnostics"    ub:"diagnostics"`
}

func buildCompileCommandResult(
	result *compilepkg.Result,
	mapper diagnostic.PathMapper,
	diagnostics []diagnostic.Diagnostic,
) (compileCommandResult, error) {
	if result == nil {
		return compileCommandResult{}, errors.New("compile result is required")
	}
	if result.Built && result.ContentRevision == "" {
		return compileCommandResult{}, errors.New("built compile result needs a content revision")
	}
	if result.Built && result.BinaryPath == "" {
		return compileCommandResult{}, errors.New("built compile result needs a binary path")
	}
	files, err := publicCompileFiles(result.Files, mapper)
	if err != nil {
		return compileCommandResult{}, err
	}
	response := compileCommandResult{
		Kind: "compile-result", FormatVersion: 1,
		Factory: compileFactoryResult{
			Name: result.FactoryName, Version: result.Version,
			LibraryPath: optionalCompileString(result.LibraryPath),
		},
		Source: compileSourceResult{
			Path:       mapper.Display(result.SourcePath),
			ProjectDir: mapper.Display(result.ProjectDir),
		},
		Output: compileOutputResult{
			Dir:    mapper.Display(result.OutputDir),
			MainGo: mapper.Display(result.MainGoPath),
			GoMod:  mapper.Display(result.GoModPath),
			Assets: optionalCompileString(mapper.Display(result.AssetsPath)),
			Built:  result.Built,
		},
		Files: files, Diagnostics: diagnostic.Normalize(diagnostics),
	}
	if result.Built {
		response.Factory.ContentRevision = optionalCompileString(result.ContentRevision)
		response.Output.Binary = optionalCompileString(mapper.Display(result.BinaryPath))
	}
	return response, nil
}

func optionalCompileString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func publicCompileFiles(
	files []filechange.Change,
	mapper diagnostic.PathMapper,
) ([]filechange.Change, error) {
	public := make([]filechange.Change, len(files))
	for index, file := range files {
		public[index] = file
		public[index].Path = mapper.Display(file.Path)
	}
	return filechange.Compose(public)
}

func compilePathMapper(cfg *compileConfig, result *compilepkg.Result) diagnostic.PathMapper {
	workingDir, _ := os.Getwd()
	mapper := diagnostic.PathMapper{WorkingDir: workingDir}
	addCompilePathMapping(&mapper, cfg.factoryPath)
	if cfg.outDir != "-" {
		addCompilePathMapping(&mapper, cfg.outDir)
	}
	addCompilePathMapping(&mapper, cfg.replaceUnobin)
	for _, replacement := range cfg.replaceGoModule {
		if _, path, ok := strings.Cut(replacement, "="); ok {
			addCompilePathMapping(&mapper, path)
		}
	}
	if result != nil {
		mapper.ProjectDir = result.ProjectDir
	}
	for _, root := range []string{
		os.TempDir(), os.Getenv("GOMODCACHE"), os.Getenv("GOCACHE"),
	} {
		if root != "" {
			mapper.HiddenRoots = append(mapper.HiddenRoots, root)
		}
	}
	return mapper
}

func addCompilePathMapping(mapper *diagnostic.PathMapper, path string) {
	if path == "" || path == "-" {
		return
	}
	display := filepath.ToSlash(filepath.Clean(path))
	absolute, err := filepath.Abs(path)
	if err != nil {
		return
	}
	mapper.Mappings = append(mapper.Mappings, diagnostic.PathMapping{
		AbsoluteRoot: absolute, DisplayRoot: display,
	})
	resolved, err := filepath.EvalSymlinks(absolute)
	if err == nil && resolved != absolute {
		mapper.Mappings = append(mapper.Mappings, diagnostic.PathMapping{
			AbsoluteRoot: resolved, DisplayRoot: display,
		})
	}
}

func writeCompileCommandFailure(
	command *cobra.Command,
	format cmdout.Format,
	collector *diagnostic.Collector,
	result *compilepkg.Result,
	mapper diagnostic.PathMapper,
	code cmdout.Code,
	err error,
) error {
	message := "compile failed"
	switch code {
	case cmdout.CodeInvalidArgs:
		message = "compile arguments are invalid"
	case cmdout.CodeIO:
		message = "compile I/O failed"
	case cmdout.CodeStdoutConflict:
		message = "compile stdout is reserved for the machine response"
	}
	defaultDiagnosticCode := "unobin.error"
	if code == cmdout.CodeIO {
		defaultDiagnosticCode = "unobin.io"
	}
	diagnostics := compileErrorDiagnostics(err, mapper, defaultDiagnosticCode)
	failure := cmdout.FailWithDiagnostics(code, message, nil, diagnostics)
	if result != nil {
		files, fileErr := publicCompileFiles(result.Files, mapper)
		if fileErr != nil {
			return fileErr
		}
		failure = cmdout.WithFiles(failure, files)
	}
	return cmdout.WriteCommandError(
		command, format, collector.Diagnostics(), failure,
	)
}

func compileErrorDiagnostics(
	err error,
	mapper diagnostic.PathMapper,
	defaultCode string,
) []diagnostic.Diagnostic {
	var pathError *os.PathError
	if errors.As(err, &pathError) {
		return []diagnostic.Diagnostic{{
			Code: defaultCode, Severity: diagnostic.SeverityError,
			Message: mapper.ReplaceKnownPrefixes(pathError.Err.Error()),
			Path:    mapper.Display(pathError.Path),
		}}
	}
	var linkError *os.LinkError
	if errors.As(err, &linkError) {
		return []diagnostic.Diagnostic{{
			Code: defaultCode, Severity: diagnostic.SeverityError,
			Message: mapper.ReplaceKnownPrefixes(linkError.Err.Error()),
			Path:    mapper.Display(linkError.New),
		}}
	}
	diagnostics := diagnostic.FromError(err, diagnostic.ConvertOptions{
		DefaultCode: defaultCode, Path: mapper.Display,
	})
	for index := range diagnostics {
		diagnostics[index].Message = mapper.ReplaceKnownPrefixes(diagnostics[index].Message)
		diagnostics[index].Hint = mapper.ReplaceKnownPrefixes(diagnostics[index].Hint)
	}
	return diagnostic.Normalize(diagnostics)
}

func compileErrorCode(err error) cmdout.Code {
	var pathError *os.PathError
	if errors.As(err, &pathError) {
		return cmdout.CodeIO
	}
	var linkError *os.LinkError
	if errors.As(err, &linkError) {
		return cmdout.CodeIO
	}
	return cmdout.CodeFailed
}

type boundedToolOutput struct {
	limit     int
	data      []byte
	truncated bool
}

func newBoundedToolOutput(limit int) *boundedToolOutput {
	return &boundedToolOutput{limit: limit}
}

func (b *boundedToolOutput) Write(value []byte) (int, error) {
	written := len(value)
	remaining := max(b.limit-len(b.data), 0)
	if len(value) > remaining {
		b.truncated = true
		value = value[:remaining]
	}
	b.data = append(b.data, value...)
	return written, nil
}

func (b *boundedToolOutput) String() string {
	value := string(completeUTF8Prefix(b.data))
	if b.truncated {
		value += fmt.Sprintf("[output truncated after %d bytes]", b.limit)
	}
	return value
}

func completeUTF8Prefix(value []byte) []byte {
	valid := 0
	for valid < len(value) {
		r, size := utf8.DecodeRune(value[valid:])
		if r == utf8.RuneError && size == 1 {
			break
		}
		valid += size
	}
	return value[:valid]
}

func reportCompileToolOutput(
	collector *diagnostic.Collector,
	mapper diagnostic.PathMapper,
	stdout *boundedToolOutput,
	stderr *boundedToolOutput,
	failed bool,
) {
	code := "unobin.compile.go-tool-output"
	severity := diagnostic.SeverityInfo
	if failed {
		code = "unobin.external-tool"
		severity = diagnostic.SeverityError
	}
	for _, stream := range []struct {
		name   string
		output *boundedToolOutput
	}{
		{name: "stdout", output: stdout},
		{name: "stderr", output: stderr},
	} {
		output := stream.output.String()
		if output == "" {
			continue
		}
		collector.Report(diagnostic.Diagnostic{
			Code: code, Severity: severity,
			Message: "go tool " + stream.name + ":\n" + mapper.ReplaceKnownPrefixes(output),
		})
	}
}
