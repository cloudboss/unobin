package runner

import (
	"slices"

	"github.com/cloudboss/unobin/pkg/diagnostic"
)

type outputsResult struct {
	Kind          string                  `json:"kind"           ub:"kind"`
	FormatVersion int                     `json:"format-version" ub:"format-version"`
	Factory       factoryIdentity         `json:"factory"        ub:"factory"`
	Stack         string                  `json:"stack"          ub:"stack"`
	Outputs       map[string]any          `json:"outputs"        ub:"outputs"`
	Sensitive     []string                `json:"sensitive"      ub:"sensitive"`
	Diagnostics   []diagnostic.Diagnostic `json:"diagnostics"    ub:"diagnostics"`
}

type outputResult struct {
	Kind          string                  `json:"kind"           ub:"kind"`
	FormatVersion int                     `json:"format-version" ub:"format-version"`
	Factory       factoryIdentity         `json:"factory"        ub:"factory"`
	Stack         string                  `json:"stack"          ub:"stack"`
	Name          string                  `json:"name"           ub:"name"`
	Value         any                     `json:"value"          ub:"value"`
	Sensitive     bool                    `json:"sensitive"      ub:"sensitive"`
	Diagnostics   []diagnostic.Diagnostic `json:"diagnostics"    ub:"diagnostics"`
}

func buildOutputsResult(
	info Info,
	stack string,
	outputs map[string]any,
	sensitive map[string]bool,
	diagnostics []diagnostic.Diagnostic,
) outputsResult {
	masked := make(map[string]any, len(outputs))
	sensitiveNames := make([]string, 0, len(sensitive))
	for name, value := range outputs {
		if sensitive[name] {
			masked[name] = sensitivePlaceholder
			sensitiveNames = append(sensitiveNames, name)
			continue
		}
		masked[name] = value
	}
	slices.Sort(sensitiveNames)
	return outputsResult{
		Kind:          "outputs",
		FormatVersion: 1,
		Factory:       factoryIdentityFor(info),
		Stack:         stack,
		Outputs:       masked,
		Sensitive:     sensitiveNames,
		Diagnostics:   diagnostic.Normalize(diagnostics),
	}
}

func buildOutputResult(
	info Info,
	stack string,
	name string,
	value any,
	sensitive bool,
	diagnostics []diagnostic.Diagnostic,
) outputResult {
	if sensitive {
		value = sensitivePlaceholder
	}
	return outputResult{
		Kind:          "output",
		FormatVersion: 1,
		Factory:       factoryIdentityFor(info),
		Stack:         stack,
		Name:          name,
		Value:         value,
		Sensitive:     sensitive,
		Diagnostics:   diagnostic.Normalize(diagnostics),
	}
}
