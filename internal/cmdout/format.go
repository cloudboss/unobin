package cmdout

import "fmt"

type Format string

const (
	FormatText   Format = "text"
	FormatJSON   Format = "json"
	FormatUnobin Format = "unobin"
)

func ParseFormat(value string) (Format, error) {
	switch value {
	case "", string(FormatText):
		return FormatText, nil
	case string(FormatJSON):
		return FormatJSON, nil
	case string(FormatUnobin):
		return FormatUnobin, nil
	default:
		return "", fmt.Errorf(
			"--format: unknown '%s' (want text, json, unobin)",
			value,
		)
	}
}

func FormatHelp() string {
	return "Output format: text, json, unobin."
}

func (f Format) Machine() bool {
	return f == FormatJSON || f == FormatUnobin
}
