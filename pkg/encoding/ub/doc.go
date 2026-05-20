// Package ub encodes Go values as unobin language literals, the
// counterpart to encoding/json for unobin's own syntax. Values that
// implement Marshaler control their own representation; everything
// else walks reflectively. Marshal emits one-line output; MarshalIndent
// adds newlines and indentation for nested containers.
package ub
