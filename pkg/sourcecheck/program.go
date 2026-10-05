package sourcecheck

import (
	"errors"
	"fmt"

	"github.com/cloudboss/unobin/pkg/asset"
	"github.com/cloudboss/unobin/pkg/diagnostic"
	"github.com/cloudboss/unobin/pkg/program"
	"github.com/cloudboss/unobin/pkg/resolve"
	"github.com/cloudboss/unobin/pkg/runtime"
)

func AnalyzeProgram(
	refs map[string]resolve.ImportRef,
	opts ImportAnalysisOptions,
) (*program.Imports, error) {
	resolver := opts.Resolver
	if opts.Mode == ModeNoFetch {
		resolver = noFetchResolver{wrapped: resolver}
	}
	schemas := opts.SchemaCache
	if schemas == nil {
		schemas = NewSchemaCache()
	}
	rootConfigDeps, err := bodyLibraryConfigDeps(opts.Body)
	if err != nil {
		return nil, err
	}
	if len(refs)+len(rootConfigDeps) > 0 && opts.Resolver == nil {
		return nil, errors.New("sourcecheck: resolver is required when dependencies are present")
	}
	visitorOpts := opts
	visitorOpts.Resolver = resolver
	preflight, compatibility, err := preflightImports(refs, visitorOpts, schemas)
	if err != nil {
		return nil, err
	}
	defer compatibility.EndAnalysis()
	metadata := compatibility.Manifest()
	schemas.compatibility = compatibility
	schemas.compatibilityErr = nil
	resolver = preflight
	visitorOpts.Resolver = resolver
	visitor := newImportVisitor(visitorOpts, schemas)
	top := preflight.graph.Top
	if err := preflight.graph.Visit(visitor); err != nil {
		return nil, err
	}
	for _, metadata := range compatibility.Manifest() {
		module := metadata.Source.Module
		if metadata.Source.Linked && module.Version != "" {
			if err := visitor.OnGoImport("", "", module.Path, module.Version); err != nil {
				return nil, err
			}
		}
	}
	rootSet, err := asset.Capture(
		opts.Source,
		opts.RootSourceFile,
		factoryBodyAssets(opts.Body),
		opts.GeneratedOutputPath,
	)
	if err != nil {
		return nil, err
	}
	if err := visitor.assets.Add(rootSet); err != nil {
		return nil, err
	}
	analysis := &program.Imports{
		Top:                  top,
		Libraries:            make(map[string]*runtime.Library, len(top)),
		LibraryConfigSchemas: map[string]runtime.LibraryConfigSchema{},
		GoModules:            visitor.goModules,
		UBLibraries:          visitor.ubLibraries,
		Assets:               visitor.assets,
		Compatibility:        compatibility,
		LibraryMetadata:      metadata,
	}
	if rootSet != nil {
		analysis.RootAssetSetID = rootSet.ID
	}
	for _, res := range top {
		switch res.Kind {
		case resolve.ResolutionGo:
			schema, warnings, err := schemas.Read(res.SourcePath)
			if err != nil {
				return nil, diagnostic.Context(
					fmt.Sprintf("import %q", res.LocalAlias), err,
				)
			}
			reportSchemaWarnings(opts.Reporter, res.LocalAlias, warnings)
			analysis.Libraries[res.LocalAlias] = &runtime.Library{Schema: schema}
		case resolve.ResolutionUB:
			analysis.Libraries[res.LocalAlias] = visitor.runtimeLibraries[res.CanonicalKey]
		}
	}
	libraryConfigSchemas, err := visitor.resolveLibraryConfigDeps(
		rootConfigDeps,
		importSourceForOptions(opts),
	)
	if err != nil {
		return nil, err
	}
	if libraryConfigSchemas != nil {
		analysis.LibraryConfigSchemas = libraryConfigSchemas
	}
	if err := preflight.graph.ValidateSources(); err != nil {
		return nil, err
	}
	if err := compatibility.ValidateSources(); err != nil {
		clear(schemas.entries)
		return nil, err
	}
	return analysis, nil
}
