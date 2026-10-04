package resolve

import "github.com/cloudboss/unobin/pkg/diagnostic"

type ModulePathError struct {
	Dependency string
	ModulePath string
	Version    string
	Commit     string
	Message    string
}

func (e *ModulePathError) Error() string {
	return e.Message
}

func (e *ModulePathError) Diagnostics() []diagnostic.Diagnostic {
	return []diagnostic.Diagnostic{{
		Code: "unobin.library-api.module-source", Severity: diagnostic.SeverityError,
		Message: e.Message,
		Hint:    "Select a release whose Go module path matches its version with `unobin deps get`.",
		LibraryCompatibility: &diagnostic.LibraryCompatibilityDetails{
			Dependency: e.Dependency, ModulePath: e.ModulePath, Version: e.Version, Commit: e.Commit,
		},
	}}
}
