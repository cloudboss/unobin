package sourcecheck

import (
	"errors"
	"fmt"
	"io"
	"testing/fstest"

	"github.com/cloudboss/unobin/pkg/check"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/resolve"
	"github.com/cloudboss/unobin/pkg/runtime"
)

// Mode controls whether remote imports may be fetched.
type Mode int

const (
	// ModeFetch uses the resolver normally.
	ModeFetch Mode = iota
	// ModeNoFetch treats uncached remote imports as opaque libraries.
	ModeNoFetch
)

// Options configures source checks.
type Options struct {
	ProjectDir  string
	Source      *resolve.Source
	Resolver    resolve.Resolver
	Versions    map[string]string
	SchemaCache *SchemaCache
	WarnOut     io.Writer
	Mode        Mode
}

// Result is the source-check result for a factory or composite body.
type Result struct {
	Libraries map[string]*runtime.Library
	DAG       *runtime.DAG
}

// CheckFactoryBody resolves imports and runs compile-time checks for body.
func CheckFactoryBody(body syntax.FactoryBody, opts Options) (*Result, error) {
	refs, errs := resolve.ExtractSyntaxBodyImports(body)
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	libs, err := buildLibraries(refs, opts)
	if err != nil {
		return nil, err
	}
	checker := check.NewSyntax(body, libs)
	if errs := checker.References(nil); errs.Len() > 0 {
		return nil, errs.Err()
	}
	if errs := checker.LiteralConstraints(); errs.Len() > 0 {
		return nil, errs.Err()
	}
	if errs := checker.ForEachNesting(); errs.Len() > 0 {
		return nil, errs.Err()
	}
	return &Result{Libraries: libs, DAG: checker.DAG()}, nil
}

// CheckUBLibrary checks every exported composite body in source.
func CheckUBLibrary(source *resolve.Source, opts Options) error {
	lib, err := resolve.ParseUBLibrarySource(source)
	if err != nil {
		return err
	}
	checkOpts := opts
	checkOpts.Source = source
	if checkOpts.ProjectDir == "" && source != nil {
		checkOpts.ProjectDir = source.Path
	}
	return checkCompositeEntries(lib.CompositeEntries(), checkOpts)
}

// CheckLibraryFile checks every exported composite body in file.
func CheckLibraryFile(file *syntax.LibraryFile, opts Options) error {
	if file == nil {
		return errors.New("sourcecheck: library file is nil")
	}
	entries := make([]resolve.CompositeEntry, 0, len(file.Exports))
	for _, export := range file.Exports {
		entries = append(entries, resolve.CompositeEntry{
			Kind:       string(export.Kind),
			Name:       export.Name.Name,
			SyntaxBody: export.Body,
		})
	}
	return checkCompositeEntries(entries, opts)
}

func checkCompositeEntries(entries []resolve.CompositeEntry, opts Options) error {
	var violations []error
	for _, entry := range entries {
		violations = append(violations,
			resolve.ValidateSyntaxCompositeBody(entry.Kind, entry.Name, entry.SyntaxBody)...)
	}
	if len(violations) > 0 {
		return errors.Join(violations...)
	}

	var bodyErrs []error
	for _, entry := range entries {
		_, err := CheckFactoryBody(entry.SyntaxBody, opts)
		if err != nil {
			bodyErrs = append(bodyErrs,
				fmt.Errorf("%s composite %q: %w", entry.Kind, entry.Name, err))
		}
	}
	return errors.Join(bodyErrs...)
}

func buildLibraries(
	refs map[string]resolve.ImportRef,
	opts Options,
) (map[string]*runtime.Library, error) {
	analysis, err := AnalyzeImports(refs, ImportAnalysisOptions{
		ProjectDir:  opts.ProjectDir,
		Source:      opts.Source,
		Resolver:    opts.Resolver,
		Versions:    opts.Versions,
		SchemaCache: opts.SchemaCache,
		WarnOut:     opts.WarnOut,
		Mode:        opts.Mode,
	})
	if err != nil {
		return nil, err
	}
	return analysis.Libraries, nil
}

func printSchemaWarnings(out io.Writer, alias string, warnings []string) {
	if out == nil {
		return
	}
	for _, warning := range warnings {
		fmt.Fprintf(out, "warning: import %q: %s\n", alias, warning)
	}
}

type noFetchResolver struct {
	wrapped resolve.Resolver
}

type noFetchSourceResolver interface {
	ResolveNoFetch(resolve.ImportRef) (*resolve.Source, bool, error)
}

func (r noFetchResolver) Resolve(ref resolve.ImportRef) (*resolve.Source, error) {
	if noFetch, ok := r.wrapped.(noFetchSourceResolver); ok {
		source, found, err := noFetch.ResolveNoFetch(ref)
		if err != nil {
			return nil, err
		}
		if found {
			return source, nil
		}
		if _, remote := ref.(*resolve.RemoteImport); remote {
			return opaqueSource(), nil
		}
	}
	return r.wrapped.Resolve(ref)
}

func (r noFetchResolver) ResolveFrom(
	ref resolve.ImportRef,
	parent *resolve.Source,
) (*resolve.Source, error) {
	if local, ok := ref.(*resolve.LocalImport); ok && parent != nil {
		return resolve.ResolveLocalSource(local, parent)
	}
	if _, remote := ref.(*resolve.RemoteImport); remote {
		return r.Resolve(ref)
	}
	if contextResolver, ok := r.wrapped.(resolve.ContextResolver); ok {
		return contextResolver.ResolveFrom(ref, parent)
	}
	return r.Resolve(ref)
}

func opaqueSource() *resolve.Source {
	return &resolve.Source{FS: fstest.MapFS{
		"library.go": &fstest.MapFile{Data: []byte("package opaque\n")},
	}}
}
