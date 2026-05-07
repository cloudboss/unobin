// Package gogen generates Go module source code from external schema formats
// (CFN registry schemas, TF provider schemas, DCL YAML). Sub-packages handle
// their own schema format; this package provides the shared types, type
// mapping, Go source rendering, and the top-level Generate orchestrator.
package gogen
