package lang

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRenderPrimitives(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want string
	}{
		{"null", nil, "null"},
		{"true", true, "true"},
		{"false", false, "false"},
		{"int64", int64(42), "42"},
		{"negative int", int64(-7), "-7"},
		{"int", int(5), "5"},
		{"float", 1.5, "1.5"},
		{"float-no-fraction", 2.0, "2"},
		{"plain string", "hello", "'hello'"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.Equal(t, c.want, Render(c.in))
		})
	}
}

func TestRenderStringEscapes(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"apostrophe", "it's", `'it\'s'`},
		{"backslash", `a\b`, `'a\\b'`},
		{"newline", "a\nb", `'a\nb'`},
		{"tab", "a\tb", `'a\tb'`},
		{"carriage-return", "a\rb", `'a\rb'`},
		{"unicode passes through", "héllo", "'héllo'"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.Equal(t, c.want, Render(c.in))
		})
	}
}

func TestRenderList(t *testing.T) {
	require.Equal(t, "[]", Render([]any{}))
	require.Equal(t, "[1, 2, 3]", Render([]any{int64(1), int64(2), int64(3)}))
	require.Equal(t, "['a', 'b']", Render([]any{"a", "b"}))
	require.Equal(t, "[1, 'two', true, null]",
		Render([]any{int64(1), "two", true, nil}))
}

func TestRenderMap(t *testing.T) {
	require.Equal(t, "{}", Render(map[string]any{}))
	require.Equal(t, "{ a: 1 }", Render(map[string]any{"a": int64(1)}))
	require.Equal(t, "{ a: 1, b: 2 }",
		Render(map[string]any{"a": int64(1), "b": int64(2)}))
	require.Equal(t, "{ kebab-key: 'v' }",
		Render(map[string]any{"kebab-key": "v"}))
	require.Equal(t, "{ 'with space': 1 }",
		Render(map[string]any{"with space": int64(1)}))
	require.Equal(t, "{ '1starts-with-digit': 1 }",
		Render(map[string]any{"1starts-with-digit": int64(1)}))
	require.Equal(t, "{ 'ends-with-': 1 }",
		Render(map[string]any{"ends-with-": int64(1)}))
}

func TestRenderNested(t *testing.T) {
	v := map[string]any{
		"name":    "web",
		"sizes":   []any{int64(1), int64(2)},
		"tags":    map[string]any{"Name": "thing"},
		"enabled": true,
	}
	require.Equal(t,
		`{ enabled: true, name: 'web', sizes: [1, 2], tags: { Name: 'thing' } }`,
		Render(v))
}

func TestRenderPrettyEmpty(t *testing.T) {
	require.Equal(t, "{}", RenderPretty(map[string]any{}))
	require.Equal(t, "[]", RenderPretty([]any{}))
}

func TestRenderPrettyPrimitivesMatchRender(t *testing.T) {
	cases := []any{nil, true, false, int64(42), 1.5, "hello", "it's"}
	for _, c := range cases {
		require.Equal(t, Render(c), RenderPretty(c))
	}
}

func TestRenderPrettyFlatMap(t *testing.T) {
	v := map[string]any{"a": int64(1), "b": "two"}
	want := "{\n  a: 1\n  b: 'two'\n}"
	require.Equal(t, want, RenderPretty(v))
}

func TestRenderPrettyFlatList(t *testing.T) {
	v := []any{int64(1), int64(2), int64(3)}
	want := "[\n  1,\n  2,\n  3,\n]"
	require.Equal(t, want, RenderPretty(v))
}

func TestRenderPrettyNested(t *testing.T) {
	v := map[string]any{
		"files": map[string]any{
			"alpha": map[string]any{
				"path": "/tmp/alpha.txt",
				"size": int64(13),
			},
			"beta": map[string]any{
				"path": "/tmp/beta.txt",
				"size": int64(14),
			},
		},
	}
	want := "{\n" +
		"  files: {\n" +
		"    alpha: {\n" +
		"      path: '/tmp/alpha.txt'\n" +
		"      size: 13\n" +
		"    }\n" +
		"    beta: {\n" +
		"      path: '/tmp/beta.txt'\n" +
		"      size: 14\n" +
		"    }\n" +
		"  }\n" +
		"}"
	require.Equal(t, want, RenderPretty(v))
}

func TestRenderPrettyReparses(t *testing.T) {
	v := map[string]any{
		"files": map[string]any{
			"alpha": map[string]any{"path": "/tmp/a", "size": int64(13)},
		},
		"tags": []any{"x", "y"},
	}
	rendered := RenderPretty(v)
	_, err := ParseSource("pretty", []byte("v: "+rendered+"\n"))
	require.NoError(t, err)
}

func TestRenderRoundTrip(t *testing.T) {
	values := []any{
		"hello",
		"it's",
		int64(42),
		1.5,
		true,
		nil,
		[]any{int64(1), "two", true, nil},
		map[string]any{"a": int64(1), "b": []any{"x", "y"}},
	}
	for _, v := range values {
		rendered := Render(v)
		t.Run(rendered, func(t *testing.T) {
			f, err := ParseSource("render", []byte("v: "+rendered+"\n"))
			require.NoError(t, err)
			require.Len(t, f.Body.Fields, 1)
		})
	}
}
