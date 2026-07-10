package cmdout

import "testing"

type formatGolden struct {
	Help  string             `json:"help"`
	Cases []formatCaseGolden `json:"cases"`
}

type formatCaseGolden struct {
	Input   string `json:"input"`
	Format  Format `json:"format"`
	Machine bool   `json:"machine"`
	Error   string `json:"error"`
}

func TestFormatGolden(t *testing.T) {
	result := formatGolden{Help: FormatHelp()}
	for _, input := range []string{"", "text", "json", "unobin", "yaml", "JSON"} {
		format, err := ParseFormat(input)
		result.Cases = append(result.Cases, formatCaseGolden{
			Input:   input,
			Format:  format,
			Machine: format.Machine(),
			Error:   cmdoutErrorString(err),
		})
	}
	requireCmdoutGolden(t, "testdata/format.json", result)
}
