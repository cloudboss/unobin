package deps

import "github.com/cloudboss/unobin/pkg/diagnostic"

type SourceSelectionError struct {
	Dependency string
	Package    string
	Version    string
	Commit     string
	Message    string
}

func (e *SourceSelectionError) Error() string {
	return e.Message
}

func (e *SourceSelectionError) Diagnostics() []diagnostic.Diagnostic {
	return []diagnostic.Diagnostic{{
		Code: "unobin.library-api.module-source", Severity: diagnostic.SeverityError,
		Message: e.Message,
		Hint:    "Select a release that provides the required package with `unobin deps get`.",
		LibraryCompatibility: &diagnostic.LibraryCompatibilityDetails{
			Dependency: e.Dependency, Package: e.Package, Version: e.Version, Commit: e.Commit,
		},
	}}
}
