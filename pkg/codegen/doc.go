// Package codegen generates Go source for compiled factories and UB libraries.
//
// It emits main.go for a factory binary and package files for imported UB
// libraries. Generated code embeds typed syntax bodies, resolved import tables,
// and compile-extracted Go library metadata that the runtime needs during plan,
// apply, validate, and refresh.
package codegen
