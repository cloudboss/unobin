package runtime

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/lang"
)

func TestOverlayDefaults(t *testing.T) {
	value := func(field, value string) lang.DefaultSpec {
		return lang.DefaultSpec{Field: field, Value: value}
	}
	tests := []struct {
		name       string
		inputs     map[string]any
		specs      []lang.DefaultSpec
		unresolved map[string][]string
		want       map[string]any
		wantErr    string
	}{
		{
			name:   "fills a missing field",
			inputs: map[string]any{"name": "a"},
			specs:  []lang.DefaultSpec{value("input.mode", "420")},
			want:   map[string]any{"name": "a", "mode": int64(420)},
		},
		{
			name:   "keeps a null field",
			inputs: map[string]any{"mode": nil},
			specs:  []lang.DefaultSpec{value("input.mode", "420")},
			want:   map[string]any{"mode": nil},
		},
		{
			name:   "keeps a set value",
			inputs: map[string]any{"mode": int64(384)},
			specs:  []lang.DefaultSpec{value("input.mode", "420")},
			want:   map[string]any{"mode": int64(384)},
		},
		{
			name:   "keeps a set zero value",
			inputs: map[string]any{"mode": int64(0)},
			specs:  []lang.DefaultSpec{value("input.mode", "420")},
			want:   map[string]any{"mode": int64(0)},
		},
		{
			name:   "keeps a set false",
			inputs: map[string]any{"on": false},
			specs:  []lang.DefaultSpec{value("input.on", "true")},
			want:   map[string]any{"on": false},
		},
		{
			name:   "fills string and boolean literals",
			inputs: map[string]any{},
			specs: []lang.DefaultSpec{
				value("input.method", "'GET'"),
				value("input.follow", "true"),
				value("input.ratio", "0.5"),
			},
			want: map[string]any{"method": "GET", "follow": true, "ratio": 0.5},
		},
		{
			name:   "fills map and list literals",
			inputs: map[string]any{},
			specs: []lang.DefaultSpec{
				value("input.tags", "{ env: 'dev' }"),
				value("input.items", "['one', 'two']"),
			},
			want: map[string]any{
				"tags":  map[string]any{"env": "dev"},
				"items": []any{"one", "two"},
			},
		},
		{
			name: "keeps set map and list values",
			inputs: map[string]any{
				"tags":  map[string]any{"env": "prod"},
				"items": []any{"explicit"},
			},
			specs: []lang.DefaultSpec{
				value("input.tags", "{ env: 'dev' }"),
				value("input.items", "['one', 'two']"),
			},
			want: map[string]any{
				"tags":  map[string]any{"env": "prod"},
				"items": []any{"explicit"},
			},
		},
		{
			name:   "optional marker fills nothing",
			inputs: map[string]any{},
			specs:  []lang.DefaultSpec{{Field: "input.dir", Optional: true}},
			want:   map[string]any{},
		},
		{
			name:   "fills a nested field when its parent is present",
			inputs: map[string]any{"code": map[string]any{"inline": "x"}},
			specs:  []lang.DefaultSpec{value("input.code.retries", "3")},
			want: map[string]any{
				"code": map[string]any{"inline": "x", "retries": int64(3)},
			},
		},
		{
			name:   "does not invent an absent parent object",
			inputs: map[string]any{},
			specs:  []lang.DefaultSpec{value("input.code.retries", "3")},
			want:   map[string]any{},
		},
		{
			name:   "does not descend into a null parent",
			inputs: map[string]any{"code": nil},
			specs:  []lang.DefaultSpec{value("input.code.retries", "3")},
			want:   map[string]any{"code": nil},
		},
		{
			name:   "does not descend into a non-object parent",
			inputs: map[string]any{"code": "inline"},
			specs:  []lang.DefaultSpec{value("input.code.retries", "3")},
			want:   map[string]any{"code": "inline"},
		},
		{
			name:       "skips a field waiting on an upstream output",
			inputs:     map[string]any{"mode": nil},
			specs:      []lang.DefaultSpec{value("input.mode", "420")},
			unresolved: map[string][]string{"mode": {"resource.core.thing.a.id"}},
			want:       map[string]any{"mode": nil},
		},
		{
			name:    "a literal that does not parse names the field",
			inputs:  map[string]any{},
			specs:   []lang.DefaultSpec{value("input.mode", "{")},
			wantErr: `default for "mode"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := overlayDefaults(tt.inputs, tt.specs, tt.unresolved)
			if tt.wantErr != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, tt.inputs)
		})
	}
}
