package program

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseBuildProfile(t *testing.T) {
	for _, test := range []struct {
		value string
		want  BuildProfile
	}{
		{value: "", want: BuildProfileFull},
		{value: "full", want: BuildProfileFull},
		{value: "local", want: BuildProfileLocal},
	} {
		profile, err := ParseBuildProfile(test.value)
		require.NoError(t, err)
		require.Equal(t, test.want, profile)
	}
	_, err := ParseBuildProfile("custom")
	require.EqualError(t, err, "unknown build profile \"custom\" (want full or local)")
}
