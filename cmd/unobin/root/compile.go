package root

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/cloudboss/unobin/internal/cmdconfig"
	"github.com/cloudboss/unobin/internal/cmdout"
	"github.com/cloudboss/unobin/pkg/compile"
	"github.com/cloudboss/unobin/pkg/diagnostic"
	"github.com/cloudboss/unobin/pkg/filechange"
	"github.com/cloudboss/unobin/pkg/program"
)

var (
	compileCfg = &compileConfig{}
	CompileCmd = &cobra.Command{
		Use:   "compile",
		Short: "Generate a factory binary's main.go from factory source",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCompile(cmd, compileCfg)
		},
	}
)

type compileConfig struct {
	factoryPath     string
	version         string
	stackName       string
	libraryPath     string
	outDir          string
	goVersion       string
	replaceUnobin   string
	replaceGoModule []string
	build           bool
	profile         string
}

func init() {
	CompileCmd.Flags().StringVar(&compileCfg.profile, "profile", "full",
		"State and encryption implementations to include: full, local.")
	CompileCmd.Flags().String("format", "text", cmdout.FormatHelp())
	CompileCmd.Flags().StringVarP(&compileCfg.factoryPath, "path", "p", ".",
		"Path to the factory source file or directory.")

	CompileCmd.Flags().StringVar(&compileCfg.version, "version", "v0.0.0",
		"Release version to stamp into the built binary.")

	CompileCmd.Flags().StringVar(&compileCfg.stackName, "name", "",
		"Stack name. Defaults to the parent directory's basename.")

	CompileCmd.Flags().StringVar(&compileCfg.libraryPath, "library-path", "",
		"Library path identity to embed in the binary. The operator's"+
			" stack file asserts the same value under factory.pin.library-path"+
			" and plan, refresh, and validate refuse on mismatch.")

	CompileCmd.Flags().StringVarP(&compileCfg.outDir, "out", "o", "",
		"Directory to write main.go and go.mod into, or `-` to print main.go to stdout.")

	CompileCmd.Flags().StringVar(&compileCfg.goVersion, "go-version", compile.GoMajorMinor(),
		"Go toolchain version to declare in the generated go.mod.")

	CompileCmd.Flags().StringVar(&compileCfg.replaceUnobin, "replace-unobin", "",
		"Local path to substitute for github.com/cloudboss/unobin via a go.mod replace directive.")

	CompileCmd.Flags().StringArrayVar(&compileCfg.replaceGoModule, "replace-go-module", nil,
		"Local replace for a Go module, repeatable. Format: `module-path=local-path`. "+
			"Both the import resolver and the generated go.mod use the substitution.")

	CompileCmd.Flags().BoolVar(&compileCfg.build, "build", false,
		"After writing the source, run `go build` in the output directory.")
}

func runCompile(cmd *cobra.Command, cfg *compileConfig) error {
	formatValue, err := cmd.Flags().GetString("format")
	if err != nil {
		return err
	}
	format, err := cmdout.ParseFormat(formatValue)
	if err != nil {
		return err
	}
	profile, err := program.ParseBuildProfile(cfg.profile)
	if err != nil {
		if format.Machine() {
			return writeCompileCommandFailure(
				cmd, format, &diagnostic.Collector{}, nil,
				compilePathMapper(cfg, nil), cmdout.CodeInvalidArgs, err,
			)
		}
		return err
	}
	replaceGoModules, err := parseReplaceFlags(cfg.replaceGoModule)
	if err != nil {
		if format.Machine() {
			return writeCompileCommandFailure(
				cmd, format, &diagnostic.Collector{}, nil,
				compilePathMapper(cfg, nil), cmdout.CodeInvalidArgs, err,
			)
		}
		return err
	}
	options := compile.Options{
		BuildProfile:         profile,
		FactoryPath:          cfg.factoryPath,
		OutDir:               cfg.outDir,
		StackName:            cfg.stackName,
		LibraryPath:          cfg.libraryPath,
		GoVersion:            cfg.goVersion,
		Version:              cfg.version,
		CLIVersion:           cliVersion(),
		LibraryAPIDescriptor: cmdconfig.LibraryAPIDescriptor,
		ReplaceUnobin:        cfg.replaceUnobin,
		ReplaceGoModules:     replaceGoModules,
		Build:                cfg.build,
		NewResolver:          cmdconfig.NewResolver,
		Stdout:               cmd.OutOrStdout(),
		Stderr:               cmd.ErrOrStderr(),
	}
	if !format.Machine() {
		return compile.Run(options)
	}
	collector := &diagnostic.Collector{}
	mapper := compilePathMapper(cfg, nil)
	if cfg.outDir == "" {
		return writeCompileCommandFailure(
			cmd, format, collector, nil, mapper, cmdout.CodeInvalidArgs,
			errors.New("--out is required (use `-` for stdout)"),
		)
	}
	if cfg.outDir == "-" {
		return writeCompileCommandFailure(
			cmd, format, collector, nil, mapper, cmdout.CodeStdoutConflict, nil,
		)
	}
	toolStdout := newBoundedToolOutput(maxCompileToolOutput)
	toolStderr := newBoundedToolOutput(maxCompileToolOutput)
	options.Stdout = toolStdout
	options.Stderr = toolStderr
	options.Reporter = collector
	result, compileErr := compile.RunResult(options)
	mapper = compilePathMapper(cfg, result)
	reportCompileToolOutput(
		collector, mapper, toolStdout, toolStderr, compileErr != nil,
	)
	if compileErr != nil {
		return writeCompileCommandFailure(
			cmd, format, collector, result, mapper,
			compileErrorCode(compileErr), compileErr,
		)
	}
	response, err := buildCompileCommandResult(result, mapper, collector.Diagnostics())
	if err != nil {
		return writeCompileCommandFailure(
			cmd, format, collector, result, mapper, cmdout.CodeFailed, err,
		)
	}
	return cmdout.WriteDocument(cmd.OutOrStdout(), format, response)
}

// parseReplaceFlags parses each `--replace-go-module module-path=local-path`
// value into the map fed to both the import resolver and the generated
// go.mod's replace directive. Returns an error on malformed entries
// (missing `=`, empty side, or relative paths -- the substitution must
// be unambiguous in go.mod and on disk).
func parseReplaceFlags(values []string) (map[string]string, error) {
	out := map[string]string{}
	for _, raw := range values {
		idx := strings.IndexByte(raw, '=')
		if idx <= 0 || idx == len(raw)-1 {
			return nil, fmt.Errorf(
				"--replace-go-module %q: expected module-path=local-path", raw)
		}
		mod := raw[:idx]
		path := raw[idx+1:]
		abs, err := filepath.Abs(path)
		if err != nil {
			return nil, diagnostic.Context(fmt.Sprintf(
				"--replace-go-module %q", raw,
			), err)
		}
		out[mod] = abs
	}
	return out, nil
}

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
	result *compile.Result,
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

func compilePathMapper(cfg *compileConfig, result *compile.Result) diagnostic.PathMapper {
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
	result *compile.Result,
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
	var provider interface {
		Diagnostics() []diagnostic.Diagnostic
	}
	hasDiagnostics := errors.As(err, &provider)
	if !hasDiagnostics && errors.As(err, &pathError) {
		return []diagnostic.Diagnostic{{
			Code: defaultCode, Severity: diagnostic.SeverityError,
			Message: mapper.ReplaceKnownPrefixes(pathError.Err.Error()),
			Path:    mapper.Display(pathError.Path),
		}}
	}
	var linkError *os.LinkError
	if !hasDiagnostics && errors.As(err, &linkError) {
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
