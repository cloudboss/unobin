package filechange

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

type filechangeGolden struct {
	Actions []Action               `json:"actions"`
	Tags    map[string][2]string   `json:"tags"`
	Sort    sortGolden             `json:"sort"`
	Compose []filechangeCaseGolden `json:"compose"`
}

type sortGolden struct {
	Input          []Change `json:"input"`
	Output         []Change `json:"output"`
	InputPreserved bool     `json:"input-preserved"`
	Copied         bool     `json:"copied"`
	EmptyNonNull   bool     `json:"empty-non-null"`
}

type filechangeCaseGolden struct {
	Name           string   `json:"name"`
	Input          []Change `json:"input"`
	Output         []Change `json:"output"`
	Error          string   `json:"error"`
	InputPreserved bool     `json:"input-preserved"`
}

func TestFilechangeContractGolden(t *testing.T) {
	typ := reflect.TypeFor[Change]()
	path, _ := typ.FieldByName("Path")
	action, _ := typ.FieldByName("Action")

	sortInput := []Change{
		{Path: "z", Action: ActionRemoved},
		{Path: "a", Action: ActionUpdated},
		{Path: "a", Action: ActionCreated},
	}
	sortBefore := append([]Change(nil), sortInput...)
	sorted := Sort(sortInput)

	result := filechangeGolden{
		Actions: []Action{
			ActionCreated,
			ActionUpdated,
			ActionRemoved,
			ActionUnchanged,
		},
		Tags: map[string][2]string{
			"action": {action.Tag.Get("json"), action.Tag.Get("ub")},
			"path":   {path.Tag.Get("json"), path.Tag.Get("ub")},
		},
		Sort: sortGolden{
			Input:          sortInput,
			Output:         sorted,
			InputPreserved: reflect.DeepEqual(sortInput, sortBefore),
			Copied:         &sortInput[0] != &sorted[0],
			EmptyNonNull:   Sort(nil) != nil,
		},
	}

	cases := []struct {
		name    string
		actions []Action
	}{
		{name: "empty"},
		{name: "created", actions: []Action{ActionCreated}},
		{name: "updated", actions: []Action{ActionUpdated}},
		{name: "removed", actions: []Action{ActionRemoved}},
		{name: "unchanged", actions: []Action{ActionUnchanged}},
		{name: "created then updated", actions: []Action{ActionCreated, ActionUpdated}},
		{name: "created then unchanged", actions: []Action{ActionCreated, ActionUnchanged}},
		{name: "created then removed", actions: []Action{ActionCreated, ActionRemoved}},
		{
			name:    "created changed then removed",
			actions: []Action{ActionCreated, ActionUpdated, ActionRemoved},
		},
		{
			name:    "updated repeatedly",
			actions: []Action{ActionUpdated, ActionUpdated, ActionUnchanged},
		},
		{
			name:    "unchanged then updated",
			actions: []Action{ActionUnchanged, ActionUpdated},
		},
		{
			name:    "unchanged repeatedly",
			actions: []Action{ActionUnchanged, ActionUnchanged},
		},
		{name: "updated then removed", actions: []Action{ActionUpdated, ActionRemoved}},
		{
			name:    "unchanged then removed",
			actions: []Action{ActionUnchanged, ActionRemoved},
		},
		{name: "removed then created", actions: []Action{ActionRemoved, ActionCreated}},
		{
			name:    "created removed then recreated",
			actions: []Action{ActionCreated, ActionRemoved, ActionCreated},
		},
		{
			name: "created removed recreated then removed",
			actions: []Action{
				ActionCreated, ActionRemoved, ActionCreated, ActionRemoved,
			},
		},
		{
			name:    "unchanged removed then recreated",
			actions: []Action{ActionUnchanged, ActionRemoved, ActionCreated},
		},
		{
			name:    "updated removed then recreated",
			actions: []Action{ActionUpdated, ActionRemoved, ActionCreated},
		},
		{
			name:    "removed recreated then updated",
			actions: []Action{ActionRemoved, ActionCreated, ActionUpdated},
		},
		{
			name:    "removed recreated then removed",
			actions: []Action{ActionRemoved, ActionCreated, ActionRemoved},
		},
		{name: "unknown action", actions: []Action{"rewritten"}},
		{name: "create existing", actions: []Action{ActionUpdated, ActionCreated}},
		{
			name:    "create unchanged existing",
			actions: []Action{ActionUnchanged, ActionCreated},
		},
		{name: "create twice", actions: []Action{ActionCreated, ActionCreated}},
		{name: "update absent", actions: []Action{ActionRemoved, ActionUpdated}},
		{
			name:    "leave absent unchanged",
			actions: []Action{ActionRemoved, ActionUnchanged},
		},
		{name: "remove absent", actions: []Action{ActionRemoved, ActionRemoved}},
		{
			name:    "remove after create and remove",
			actions: []Action{ActionCreated, ActionRemoved, ActionRemoved},
		},
		{
			name:    "unchanged after create and remove",
			actions: []Action{ActionCreated, ActionRemoved, ActionUnchanged},
		},
		{
			name:    "update after create and remove",
			actions: []Action{ActionCreated, ActionRemoved, ActionUpdated},
		},
	}
	for _, tc := range cases {
		input := make([]Change, len(tc.actions))
		for i, action := range tc.actions {
			input[i] = Change{Path: "file.ub", Action: action}
		}
		before := append([]Change{}, input...)
		output, err := Compose(input)
		result.Compose = append(result.Compose, filechangeCaseGolden{
			Name:           tc.name,
			Input:          input,
			Output:         output,
			Error:          errorString(err),
			InputPreserved: reflect.DeepEqual(input, before),
		})
	}

	multiple := []Change{
		{Path: "z.ub", Action: ActionCreated},
		{Path: "a.ub", Action: ActionUnchanged},
		{Path: "gone.ub", Action: ActionCreated},
		{Path: "z.ub", Action: ActionUpdated},
		{Path: "gone.ub", Action: ActionRemoved},
	}
	multipleOutput, err := Compose(multiple)
	result.Compose = append(result.Compose, filechangeCaseGolden{
		Name:           "multiple paths",
		Input:          multiple,
		Output:         multipleOutput,
		Error:          errorString(err),
		InputPreserved: true,
	})

	requireJSONGolden(t, "testdata/filechange.json", result)
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func requireJSONGolden(t *testing.T, path string, value any) {
	t.Helper()
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	require.NoError(t, encoder.Encode(value))
	got := buffer.Bytes()
	want, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, string(want), string(got))
}
