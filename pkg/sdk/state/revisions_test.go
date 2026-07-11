package state

import (
	"encoding/json"
	"os"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

type revisionSortGolden struct {
	Input          []string `json:"input"`
	Output         []string `json:"output"`
	InputPreserved bool     `json:"input-preserved"`
	EmptyNonNull   bool     `json:"empty-non-null"`
}

func TestSortRevisionsGolden(t *testing.T) {
	input := []string{
		"2026-07-10T12:00:00Z_10",
		"invalid-z",
		"2026-07-10T12:00:00.1Z",
		"2026-07-10T12:00:00Z_2",
		"2026-07-10T12:00:01Z",
		"2026-07-10T12:00:00Z",
		"invalid-a",
	}
	before := slices.Clone(input)
	result := revisionSortGolden{
		Input:          input,
		Output:         SortRevisions(input),
		InputPreserved: slices.Equal(input, before),
		EmptyNonNull:   SortRevisions(nil) != nil,
	}
	got, err := json.MarshalIndent(result, "", "  ")
	require.NoError(t, err)
	got = append(got, '\n')
	want, err := os.ReadFile("testdata/revision-sort.json")
	require.NoError(t, err)
	require.Equal(t, string(want), string(got))
}
