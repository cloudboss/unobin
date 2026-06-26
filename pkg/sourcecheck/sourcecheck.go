package sourcecheck

import (
	"errors"
	"fmt"
	"io"
	"os"
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
	entries := lib.CompositeEntries()
	var violations []error
	for _, entry := range entries {
		violations = append(violations,
			resolve.ValidateSyntaxCompositeBody(entry.Kind, entry.Name, entry.SyntaxBody)...)
	}
	if len(violations) > 0 {
		return errors.Join(violations...)
	}

	checkOpts := opts
	checkOpts.Source = source
	if checkOpts.ProjectDir == "" && source != nil {
		checkOpts.ProjectDir = source.Path
	}
	var bodyErrs []error
	for _, entry := range entries {
		_, err := CheckFactoryBody(entry.SyntaxBody, checkOpts)
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
	if len(refs) > 0 && opts.Resolver == nil {
		return nil, errors.New("sourcecheck: resolver is required when imports are present")
	}
	resolver := opts.Resolver
	if opts.Mode == ModeNoFetch {
		resolver = noFetchResolver{wrapped: resolver}
	}
	schemas := opts.SchemaCache
	if schemas == nil {
		schemas = NewSchemaCache()
	}
	visitor := &libraryVisitor{
		byKey:   map[string]*runtime.Library{},
		warnOut: opts.WarnOut,
		schemas: schemas,
	}
	top, err := resolve.WalkUBFrom(refs, resolver, visitor, opts.Versions, sourceForOptions(opts))
	if err != nil {
		return nil, err
	}
	libs := make(map[string]*runtime.Library, len(top))
	for _, res := range top {
		switch res.Kind {
		case resolve.ResolutionGo:
			schema, warnings, err := schemas.Read(res.SourcePath)
			if err != nil {
				return nil, fmt.Errorf("import %q: %w", res.LocalAlias, err)
			}
			printSchemaWarnings(opts.WarnOut, res.LocalAlias, warnings)
			libs[res.LocalAlias] = &runtime.Library{Schema: schema}
		case resolve.ResolutionUB:
			libs[res.LocalAlias] = visitor.byKey[res.CanonicalKey]
		}
	}
	return libs, nil
}

func sourceForOptions(opts Options) *resolve.Source {
	if opts.Source != nil {
		return opts.Source
	}
	if opts.ProjectDir == "" {
		return nil
	}
	return &resolve.Source{FS: os.DirFS(opts.ProjectDir), Path: opts.ProjectDir}
}

type libraryVisitor struct {
	byKey   map[string]*runtime.Library
	warnOut io.Writer
	schemas *SchemaCache
}

func (v *libraryVisitor) OnGoImport(_, _, _, _ string) error {
	return nil
}

func (v *libraryVisitor) OnUBLibrary(
	alias, canonicalKey string, _ resolve.ImportRef, lib *resolve.UBLibrary,
) error {
	runtimeLib := &runtime.Library{Name: alias}
	for _, entry := range lib.CompositeEntries() {
		resols := lib.BodyImports[entry.Kind][entry.Name]
		bodyLibs := make(map[string]*runtime.Library, len(resols))
		for _, res := range resols {
			switch res.Kind {
			case resolve.ResolutionGo:
				schema, warnings, err := v.schemas.Read(res.SourcePath)
				if err != nil {
					return fmt.Errorf(
						"%s composite %q import %q: %w",
						entry.Kind, entry.Name, res.LocalAlias, err)
				}
				printSchemaWarnings(v.warnOut, res.LocalAlias, warnings)
				bodyLibs[res.LocalAlias] = &runtime.Library{Schema: schema}
			case resolve.ResolutionUB:
				bodyLibs[res.LocalAlias] = v.byKey[res.CanonicalKey]
			}
		}
		syntaxBody := entry.SyntaxBody
		runtimeLib.AddComposite(&runtime.CompositeType{
			Name:       entry.Name,
			Kind:       runtime.NodeKind(entry.Kind),
			SyntaxBody: &syntaxBody,
			Libraries:  bodyLibs,
		})
	}
	v.byKey[canonicalKey] = runtimeLib
	return nil
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
