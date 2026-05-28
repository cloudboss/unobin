package runner

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/cloudboss/unobin/pkg/encoding/ub"
)

// Format is the wire form a subcommand emits. The text form is the
// existing human-facing rendering; json emits NDJSON envelopes (one
// per line); unobin emits the same envelope shape encoded as UB
// literals, one per line.
type Format string

const (
	FormatText   Format = "text"
	FormatJSON   Format = "json"
	FormatUnobin Format = "unobin"
)

// ParseFormat resolves the value of the --output flag. An empty
// string maps to text so commands with the flag-default of "" pick up
// the same behavior as commands without the flag.
func ParseFormat(s string) (Format, error) {
	switch s {
	case "", "text":
		return FormatText, nil
	case "json":
		return FormatJSON, nil
	case "unobin":
		return FormatUnobin, nil
	}
	return "", fmt.Errorf("--output: unknown %q (want text, json, or unobin)", s)
}

// writeEnvelope serializes env in the chosen format and writes a
// single line followed by '\n'. The text form has no envelope shape;
// callers handle text rendering themselves.
func writeEnvelope(out io.Writer, format Format, env any) error {
	var (
		b   []byte
		err error
	)
	switch format {
	case FormatJSON:
		b, err = json.Marshal(env)
	case FormatUnobin:
		b, err = ub.Marshal(env)
	default:
		return fmt.Errorf("writeEnvelope: format %q has no envelope form", format)
	}
	if err != nil {
		return err
	}
	if _, err := out.Write(b); err != nil {
		return err
	}
	_, err = out.Write([]byte{'\n'})
	return err
}

type applyEventEnv struct {
	Kind     string `json:"kind"               ub:"kind"`
	Time     string `json:"time"               ub:"time"`
	Stage    string `json:"stage"              ub:"stage"`
	Decision string `json:"decision"           ub:"decision"`
	Address  string `json:"address"            ub:"address"`
	Elapsed  string `json:"elapsed,omitempty"  ub:"elapsed,omitempty"`
	Err      string `json:"err,omitempty"      ub:"err,omitempty"`
}

type applyOutputEnv struct {
	Kind  string `json:"kind"  ub:"kind"`
	Name  string `json:"name"  ub:"name"`
	Value any    `json:"value" ub:"value"`
}

type applyErrorEnv struct {
	Kind      string `json:"kind"                  ub:"kind"`
	Address   string `json:"address"               ub:"address"`
	Decision  string `json:"decision,omitempty"    ub:"decision,omitempty"`
	Library   string `json:"library,omitempty"      ub:"library,omitempty"`
	Elapsed   string `json:"elapsed,omitempty"     ub:"elapsed,omitempty"`
	Err       string `json:"err"                   ub:"err"`
	Skipped   int    `json:"skipped,omitempty"     ub:"skipped,omitempty"`
	Succeeded int    `json:"succeeded,omitempty"   ub:"succeeded,omitempty"`
}
