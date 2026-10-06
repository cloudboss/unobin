package state

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSnapshotCategoryAndComposition(t *testing.T) {
	for _, category := range []string{"resource", "data-source", "action"} {
		for _, composite := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/composite=%t", category, composite), func(t *testing.T) {
				body, err := json.Marshal(map[string]any{
					"format-version": 2,
					"entries": []any{map[string]any{
						"address": category + ".example", "category": category,
						"composite": composite,
						"binding":   map[string]any{"alias": "library", "kind": "example"},
					}},
				})
				require.NoError(t, err)
				snapshot, err := DecodeSnapshot(body)
				require.NoError(t, err)
				encoded, err := EncodeSnapshot(snapshot)
				require.NoError(t, err)
				var got map[string]any
				require.NoError(t, json.Unmarshal(encoded, &got))
				entry := got["entries"].([]any)[0].(map[string]any)
				require.Equal(t, category, entry["category"])
				require.Equal(t, composite, entry["composite"])
				require.NotContains(t, entry, "entry-kind")
			})
		}
	}
}

func TestSnapshotRejectsOlderFormats(t *testing.T) {
	for _, version := range []int{0, 1} {
		body := fmt.Appendf(nil, `{"format-version":%d,"entries":[{`+
			`"address":"resource.old","entry-kind":"leaf","category":"resource"}]}`, version)
		_, err := DecodeSnapshot(body)
		require.EqualError(t, err, fmt.Sprintf(
			"snapshot: unsupported format-version %d (this build expects 2); recreate the state", version))
	}
}

func TestSnapshotRequiresExplicitComposition(t *testing.T) {
	for _, value := range []string{"", `,"composite":null`} {
		body := []byte(`{"format-version":2,"entries":[{` +
			`"address":"resource.example","category":"resource",` +
			`"binding":{"alias":"library","kind":"example"}` + value + `}]}`)
		_, err := DecodeSnapshot(body)
		require.ErrorContains(t, err, `entry "resource.example" missing composite`)
	}
}
