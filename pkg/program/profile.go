package program

import "fmt"

type BuildProfile string

const (
	BuildProfileFull  BuildProfile = "full"
	BuildProfileLocal BuildProfile = "local"
)

func ParseBuildProfile(value string) (BuildProfile, error) {
	switch BuildProfile(value) {
	case "", BuildProfileFull:
		return BuildProfileFull, nil
	case BuildProfileLocal:
		return BuildProfileLocal, nil
	default:
		return "", fmt.Errorf("unknown build profile %q (want full or local)", value)
	}
}
