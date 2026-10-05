package project

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/cloudboss/unobin/pkg/check"
	"github.com/cloudboss/unobin/pkg/compile"
	"github.com/cloudboss/unobin/pkg/deps"
	"github.com/cloudboss/unobin/pkg/diagnostic"
	"github.com/cloudboss/unobin/pkg/resolve"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sourcecheck"
)

func SourceGraph(
	options Options,
	reporter diagnostic.Reporter,
) (*runtime.DAG, string, error) {
	if options.NewResolver == nil {
		options.NewResolver = compile.NewProjectResolver
	}
	stackPath, err := compile.FactorySourcePath(options.Path)
	if err != nil {
		return nil, "", err
	}
	src, err := os.ReadFile(stackPath)
	if err != nil {
		return nil, "", err
	}
	sf, _, err := compile.ParseFactorySyntaxSource(stackPath, src)
	if err != nil {
		return nil, "", err
	}

	refs, errs := resolve.ExtractSyntaxBodyImports(sf.Factory.Body)
	if len(errs) > 0 {
		return nil, "", errors.Join(errs...)
	}

	projectDir, err := sourceProjectDir(filepath.Dir(stackPath))
	if err != nil {
		return nil, "", err
	}
	project, err := sourceProject(projectDir)
	if err != nil {
		return nil, "", err
	}
	var replaceMap map[deps.Dependency]string
	if project != nil {
		if err := deps.CheckReplacementSentinels(project); err != nil {
			return nil, "", err
		}
		replaceMap = project.Replace
	}
	replaceUnobin, err := UnobinReplacement(projectDir, options.ReplaceUnobin, replaceMap)
	if err != nil {
		return nil, "", err
	}
	compatibility, err := options.Compatibility(
		projectDir, project, replaceUnobin)
	if err != nil {
		return nil, "", err
	}

	projectLock, err := readProjectLockOrNil(projectDir)
	if err != nil {
		return nil, "", err
	}
	resolver, err := options.NewResolver(projectDir)
	if err != nil {
		return nil, "", err
	}
	resolver = compile.WrapProjectLockSources(resolver, projectLock)
	resolver, err = compile.WrapReplaces(resolver, projectDir, options.ReplaceUnobin, replaceMap)
	if err != nil {
		return nil, "", err
	}

	repoVersions, err := compile.ProjectLockVersions(projectDir)
	if err != nil {
		return nil, "", err
	}
	repoVersions = replacedVersions(
		repoVersions, options.ReplaceUnobin != "", replaceMap)
	schemaRoots := compile.UnobinSchemaRoots(
		options.toolOutput(), replaceUnobin, options.UnobinVersion)
	analysis, err := sourcecheck.AnalyzeProgram(refs, sourcecheck.ImportAnalysisOptions{
		Resolver:       resolver,
		Versions:       repoVersions,
		Reporter:       reporter,
		SchemaCache:    compile.NewSchemaCacheWithCompatibility(compatibility, schemaRoots...),
		Body:           &sf.Factory.Body,
		RootSourceFile: sourceFileForProject(projectDir, stackPath),
		Source:         sourceForProjectDir(projectDir, filepath.Dir(stackPath)),
	})
	if err != nil {
		return nil, "", err
	}
	libs := analysis.Libraries
	checker := check.NewSyntaxWithLibraryConfigSchemas(
		sf.Factory.Body,
		libs,
		analysis.LibraryConfigSchemas,
		analysis.Assets.Catalog(),
		analysis.RootAssetSetID,
	)
	if errs := checker.References(nil); errs.Len() > 0 {
		return nil, "", errs.Err()
	}
	return checker.DAG(), compile.DeriveStackName(stackPath), nil
}
