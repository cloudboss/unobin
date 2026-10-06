package lsp

import (
	"os"
	"sync"

	"github.com/cloudboss/unobin/pkg/check"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/program"
	"github.com/cloudboss/unobin/pkg/resolve"
	"github.com/cloudboss/unobin/pkg/sourcecheck"
	"github.com/cloudboss/unobin/pkg/typecheck"
)

type bodyAnalysis struct {
	path    string
	body    *syntax.FactoryBody
	library *syntax.LibraryFile
	once    sync.Once
	result  *program.CheckedBody
	err     error
	imports map[string]resolvedImport
}

func (a *bodyAnalysis) checked(projects *ProjectCache) (*program.CheckedBody, error) {
	a.once.Do(func() {
		opts, ok, err := diagnosticSourceCheckOptions(a.path, projects)
		if err != nil {
			a.err = err
			return
		}
		if !ok && a.library == nil {
			a.checkOpaque()
			return
		}
		if !ok {
			opts = diagnosticLooseLibraryOptions(a.path, a.library)
		}
		a.result, a.err = sourcecheck.AnalyzeFactoryBody(*a.body, opts)
		if a.result == nil || !ok {
			return
		}
		project, err := projects.ProjectForPath(a.path)
		if err != nil {
			a.err = err
			return
		}
		a.imports = make(map[string]resolvedImport, len(a.result.Imports.Top))
		for _, res := range a.result.Imports.Top {
			resolved := resolvedImport{project: project, found: true, sourceOK: res.SourcePath != ""}
			if resolved.sourceOK {
				resolved.source = &resolve.Source{
					FS: os.DirFS(res.SourcePath), Path: res.SourcePath,
					ModulePath: res.ModulePath, ModuleRootPath: res.ModuleRootPath,
					GoImportPath: res.GoImportPath,
				}
			}
			if res.Kind == resolve.ResolutionGo {
				if library := a.result.Imports.Libraries[res.LocalAlias]; library != nil {
					resolved.schema = library.Schema
				}
			}
			a.imports[res.LocalAlias] = resolved
		}
	})
	return a.result, a.err
}

func (a *bodyAnalysis) checkOpaque() {
	libs := opaqueImportedLibraries(*a.body)
	checker := check.NewSyntax(*a.body, libs, nil, "")
	a.result = &program.CheckedBody{
		Imports: &program.Imports{Libraries: libs}, DAG: checker.DAG(),
		LocalTypes: make(map[string]typecheck.Type, len(a.body.Locals)),
	}
	locals := make(map[lang.Expr]string, len(a.body.Locals))
	for _, local := range a.body.Locals {
		if local.Value != nil {
			locals[local.Value] = local.Name.Name
		}
	}
	a.err = checker.References(func(expr lang.Expr, typ typecheck.Type) {
		if name, ok := locals[expr]; ok {
			a.result.LocalTypes[name] = typ
		}
	}).Err()
}
