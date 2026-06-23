package lang

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
)

func formatString(t *testing.T, src string) string {
	t.Helper()
	f, err := ParseSource("test.ub", []byte(src))
	require.NoError(t, err, "parse failed for source:\n%s", src)
	out, err := Format(f)
	require.NoError(t, err, "format failed")
	return string(out)
}

func TestFormatFixtures(t *testing.T) {
	ubtest.Run(t, "testdata/ub/format/valid",
		func(name string, src []byte) (string, []string) {
			f, err := ParseSource(name+".ub", src)
			if err != nil {
				return "", []string{err.Error()}
			}
			out, err := Format(f)
			if err != nil {
				return "", []string{err.Error()}
			}
			return string(out), nil
		},
		ubtest.Idempotent(),
		ubtest.Repeat(5),
	)
}

func TestFormatTypeExpressionFixtures(t *testing.T) {
	ubtest.Run(t, "testdata/ub/format-types/valid",
		func(name string, src []byte) (string, []string) {
			te, err := ParseType(name+".ub", bytes.TrimSpace(src))
			if err != nil {
				return "", []string{err.Error()}
			}
			out, err := Format(&File{Body: &ObjectLit{Fields: []*Field{{
				Key:   FieldKey{Kind: FieldIdent, Name: "t"},
				Value: te,
			}}}})
			if err != nil {
				return "", []string{err.Error()}
			}
			return strings.TrimPrefix(string(out), "t: "), nil
		},
		ubtest.Idempotent(),
		ubtest.Repeat(5),
	)
}

func TestFormatMaxColumn24Fixtures(t *testing.T) {
	runFormatMaxColumnFixtures(t, "testdata/ub/format/max-column-24/valid", 24)
}

func TestFormatMaxColumn30Fixtures(t *testing.T) {
	runFormatMaxColumnFixtures(t, "testdata/ub/format/max-column-30/valid", 30)
}

func TestFormatMaxColumn50Fixtures(t *testing.T) {
	runFormatMaxColumnFixtures(t, "testdata/ub/format/max-column-50/valid", 50)
}

func TestFormatWrapStrings60Fixtures(t *testing.T) {
	ubtest.Run(t, "testdata/ub/format/wrap-strings-60/valid",
		func(name string, src []byte) (string, []string) {
			f, err := ParseSource(name+".ub", src)
			if err != nil {
				return "", []string{err.Error()}
			}
			out, err := FormatWith(f, FormatOptions{MaxColumn: 60, WrapStrings: true})
			if err != nil {
				return "", []string{err.Error()}
			}
			return string(out), nil
		},
		ubtest.Idempotent(),
		ubtest.Repeat(5),
	)
}

func runFormatMaxColumnFixtures(t *testing.T, dir string, maxColumn int) {
	t.Helper()
	ubtest.Run(t, dir,
		func(name string, src []byte) (string, []string) {
			f, err := ParseSource(name+".ub", src)
			if err != nil {
				return "", []string{err.Error()}
			}
			out, err := FormatWith(f, FormatOptions{MaxColumn: maxColumn})
			if err != nil {
				return "", []string{err.Error()}
			}
			return string(out), nil
		},
		ubtest.Idempotent(),
		ubtest.Repeat(5),
	)
}

func TestFormatWithMaxColumn(t *testing.T) {
	tests := []struct {
		name      string
		maxColumn int
		src       string
		want      string
	}{
		{
			name:      "default_keeps_array_inline",
			maxColumn: 0,
			src:       "items: [1, 2, 3, 4, 5, 6, 7]\n",
			want:      "items: [1, 2, 3, 4, 5, 6, 7]\n",
		},
		{
			name:      "default_uses_100_when_unset",
			maxColumn: 0,
			src:       "x: 'a'\n",
			want:      "x: 'a'\n",
		},
		{
			name:      "max_28_packs_14_atoms_into_5_5_4",
			maxColumn: 28,
			src:       "items: ['a', 'a', 'a', 'a', 'a', 'a', 'a', 'a', 'a', 'a', 'a', 'a', 'a', 'a']\n",
			want:      "items: [\n  'a', 'a', 'a', 'a', 'a',\n  'a', 'a', 'a', 'a', 'a',\n  'a', 'a', 'a', 'a',\n]\n",
		},
		{
			name:      "tight_max_packs_evenly_into_three_per_line",
			maxColumn: 12,
			src:       "items: [1, 2, 3, 4, 5, 6, 7]\n",
			want:      "items: [\n  1, 2, 3,\n  4, 5, 6,\n  7,\n]\n",
		},
		{
			name:      "single_atom_wider_than_max_still_renders",
			maxColumn: 4,
			src:       "items: ['abcdef']\n",
			want:      "items: [\n  'abcdef',\n]\n",
		},
		{
			name:      "negative_max_falls_back_to_default",
			maxColumn: -1,
			src:       "x: 'hi'\n",
			want:      "x: 'hi'\n",
		},
		{
			name:      "folded_triple_quote_respects_tight_max",
			maxColumn: 20,
			src:       "msg: '''>\n  one two three four five six seven eight nine ten\n  '''\n",
			want:      "msg: '''>\n  one two three four\n  five six seven\n  eight nine ten\n  '''\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file, err := ParseSource("t.ub", []byte(tt.src))
			require.NoError(t, err)
			got, err := FormatWith(file, FormatOptions{MaxColumn: tt.maxColumn})
			require.NoError(t, err)
			require.Equal(t, tt.want, string(got))
		})
	}
}

func TestFormatWithWrapStrings(t *testing.T) {
	tests := []struct {
		name        string
		maxColumn   int
		wrapStrings bool
		src         string
		want        string
	}{
		{
			name:        "off_by_default_keeps_long_single_quoted",
			maxColumn:   0,
			wrapStrings: false,
			src:         "msg: 'this is an intentionally long single quoted string that goes well past the line budget by a lot'\n",
			want:        "msg: 'this is an intentionally long single quoted string that goes well past the line budget by a lot'\n",
		},
		{
			name:        "on_short_stays_single_quoted",
			maxColumn:   0,
			wrapStrings: true,
			src:         "msg: 'short string'\n",
			want:        "msg: 'short string'\n",
		},
		{
			name:        "on_long_with_spaces_becomes_folded",
			maxColumn:   40,
			wrapStrings: true,
			src:         "msg: 'this is a fairly long sentence that does not fit on a forty char line'\n",
			want:        "msg: '''>-\n  this is a fairly long sentence that\n  does not fit on a forty char line\n  '''\n",
		},
		{
			name:        "on_long_without_spaces_becomes_joined",
			maxColumn:   40,
			wrapStrings: true,
			src:         "url: 'https://example.com/api/v2/very/long/path/that/needs/breaking/up'\n",
			want:        "url: '''\\-\n  https://example.com/api/v2/very/\n  long/path/that/needs/breaking/up\n  '''\n",
		},
		{
			// Greedy would pack the first line full (24) and leave the
			// second short (14); the even wrap balances them at 19 each.
			name:        "on_long_folded_distributes_evenly",
			maxColumn:   26,
			wrapStrings: true,
			src:         "msg: 'aaaa bbbb cccc dddd eeee ffff gggg hhhh'\n",
			want:        "msg: '''>-\n  aaaa bbbb cccc dddd\n  eeee ffff gggg hhhh\n  '''\n",
		},
		{
			name:        "on_with_triple_quote_in_body_stays_single_quoted",
			maxColumn:   30,
			wrapStrings: true,
			src:         "msg: 'contains triple-quote \\'\\'\\' in the middle of a longer body'\n",
			want:        "msg: 'contains triple-quote \\'\\'\\' in the middle of a longer body'\n",
		},
		{
			name:        "off_long_folded_input_still_rewraps_at_max",
			maxColumn:   30,
			wrapStrings: false,
			src:         "msg: '''>\n  one two three four five six seven eight nine ten eleven\n  '''\n",
			want:        "msg: '''>\n  one two three four five six\n  seven eight nine ten eleven\n  '''\n",
		},
		{
			name:        "off_long_literal_does_not_rewrap",
			maxColumn:   20,
			wrapStrings: false,
			src:         "msg: '''|\n  this exact line stays as it is even if too long\n  '''\n",
			want:        "msg: '''|\n  this exact line stays as it is even if too long\n  '''\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file, err := ParseSource("t.ub", []byte(tt.src))
			require.NoError(t, err)
			got, err := FormatWith(file, FormatOptions{
				MaxColumn:   tt.maxColumn,
				WrapStrings: tt.wrapStrings,
			})
			require.NoError(t, err)
			require.Equal(t, tt.want, string(got))
		})
	}
}

func TestWordWrap(t *testing.T) {
	tests := []struct {
		name  string
		in    string
		width int
		want  []string
	}{
		{
			name:  "empty",
			in:    "",
			width: 10,
			want:  nil,
		},
		{
			name:  "single word fits",
			in:    "hello",
			width: 10,
			want:  []string{"hello"},
		},
		{
			name:  "single word exact",
			in:    "hello",
			width: 5,
			want:  []string{"hello"},
		},
		{
			name:  "single word overflows",
			in:    "hello",
			width: 3,
			want:  []string{"hello"},
		},
		{
			name:  "no spaces overflows",
			in:    "supercalifragilistic",
			width: 8,
			want:  []string{"supercalifragilistic"},
		},
		{
			name:  "two words one line",
			in:    "aa bb",
			width: 10,
			want:  []string{"aa bb"},
		},
		{
			name:  "two words exact fit",
			in:    "aaaa bbbb",
			width: 9,
			want:  []string{"aaaa bbbb"},
		},
		{
			name:  "two words must split",
			in:    "aaaa bbbb",
			width: 5,
			want:  []string{"aaaa", "bbbb"},
		},
		{
			name:  "fits whole on wide line",
			in:    "aaaa bbbb cccc",
			width: 100,
			want:  []string{"aaaa bbbb cccc"},
		},
		{
			name:  "balances two lines",
			in:    "aaaa bbbb cccc dddd eeee ffff gggg hhhh",
			width: 24,
			want:  []string{"aaaa bbbb cccc dddd", "eeee ffff gggg hhhh"},
		},
		{
			name:  "balances six words",
			in:    "xxxx xxxx xxxx xxxx xxxx xxxx",
			width: 20,
			want:  []string{"xxxx xxxx xxxx", "xxxx xxxx xxxx"},
		},
		{
			name:  "balances three lines",
			in:    "xxxx xxxx xxxx xxxx xxxx xxxx xxxx xxxx xxxx",
			width: 14,
			want:  []string{"xxxx xxxx xxxx", "xxxx xxxx xxxx", "xxxx xxxx xxxx"},
		},
		{
			name:  "balances four lines",
			in:    "xxxx xxxx xxxx xxxx xxxx xxxx xxxx xxxx xxxx xxxx xxxx xxxx",
			width: 14,
			want: []string{
				"xxxx xxxx xxxx", "xxxx xxxx xxxx", "xxxx xxxx xxxx", "xxxx xxxx xxxx",
			},
		},
		{
			name:  "balances uneven word lengths",
			in:    "aaaaaa bb cc dd",
			width: 10,
			want:  []string{"aaaaaa", "bb cc dd"},
		},
		{
			name:  "long word among short ones",
			in:    "a bbbbbbbbbb c",
			width: 6,
			want:  []string{"a", "bbbbbbbbbb", "c"},
		},
		{
			name:  "width one breaks every word",
			in:    "ab cd",
			width: 1,
			want:  []string{"ab", "cd"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, wordWrap(tt.in, tt.width))
		})
	}
}

// Every wrapped line stays within width except a single word that is
// itself wider than width, which gets its own overflowing line.
func TestWordWrapRespectsWidth(t *testing.T) {
	in := "alpha bravo charlie delta echo foxtrot golf hotel india juliet"
	for width := 5; width <= 40; width++ {
		for _, line := range wordWrap(in, width) {
			if strings.Contains(line, " ") {
				require.LessOrEqual(t, len(line), width,
					"multi-word line %q exceeds width %d", line, width)
			}
		}
	}
}

func TestWordWrapDeterministic(t *testing.T) {
	inputs := []struct {
		in    string
		width int
	}{
		{"aaaa bbbb cccc dddd eeee ffff gggg hhhh", 24},
		{"xxxx xxxx xxxx xxxx xxxx xxxx", 20},
		{"aaaaaa bb cc dd", 10},
	}
	for _, tt := range inputs {
		first := wordWrap(tt.in, tt.width)
		for i := range 5 {
			require.Equal(t, first, wordWrap(tt.in, tt.width), "iteration %d differs", i)
		}
	}
}

var smartColumnBreakCases = []struct {
	name  string
	input string
	width int
	want  []string
}{
	{
		name:  "empty",
		input: "",
		width: 30,
		want:  nil,
	},
	{
		name:  "single char",
		input: "x",
		width: 30,
		want:  []string{"x"},
	},
	{
		name:  "exactly width",
		input: "0123456789",
		width: 10,
		want:  []string{"0123456789"},
	},
	{
		name:  "shorter than width",
		input: "https://example.com/short",
		width: 50,
		want:  []string{"https://example.com/short"},
	},
	{
		name:  "one over width with no break char",
		input: "abcdefghijk",
		width: 10,
		want:  []string{"abcdef", "ghijk"},
	},
	{
		name:  "url breaks at a slash near midpoint",
		input: "https://example.com/api/v1/resources/12345/details",
		width: 30,
		want:  []string{"https://example.com/api/", "v1/resources/12345/details"},
	},
	{
		name:  "url with query string breaks at dot then ampersand",
		input: "https://example.com/search?q=hello&lang=en&limit=20",
		width: 25,
		want:  []string{"https://example.", "com/search?q=hello&", "lang=en&limit=20"},
	},
	{
		name:  "url ending in fragment breaks at slash near midpoint",
		input: "https://example.com/docs/guide/intro#section-three",
		width: 25,
		want:  []string{"https://example.com/docs/", "guide/intro#section-three"},
	},
	{
		name:  "arn breaks at the dash closest to the midpoint",
		input: "arn:aws:s3:::very-long-bucket-name/key/inside/the/bucket",
		width: 30,
		want:  []string{"arn:aws:s3:::very-long-bucket-", "name/key/inside/the/bucket"},
	},
	{
		name:  "unix path breaks at slash then dash",
		input: "/usr/local/share/applications/something-with-a-long-name.desktop",
		width: 25,
		want:  []string{"/usr/local/share/", "applications/something-", "with-a-long-name.desktop"},
	},
	{
		name:  "comma list breaks at commas",
		input: "alpha,beta,gamma,delta,epsilon,zeta,eta,theta,iota,kappa,lambda,mu",
		width: 30,
		want:  []string{"alpha,beta,gamma,delta,", "epsilon,zeta,eta,theta,", "iota,kappa,lambda,mu"},
	},
	{
		name:  "blob with no break chars cuts evenly",
		input: "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789",
		width: 20,
		want:  []string{"abcdefghijklmnop", "qrstuvwxyzABCDEF", "GHIJKLMNOPQRSTUV", "WXYZ0123456789"},
	},
	{
		name:  "blob exact multiple of width cuts at width",
		input: "AAAAAAAAAABBBBBBBBBBCCCCCCCCCCDDDDDDDDDD",
		width: 10,
		want:  []string{"AAAAAAAAAA", "BBBBBBBBBB", "CCCCCCCCCC", "DDDDDDDDDD"},
	},
	{
		name:  "blob one over width multiple takes an extra line",
		input: "AAAAAAAAAABBBBBBBBBBCCCCCCCCCCDDDDDDDDDDX",
		width: 10,
		want:  []string{"AAAAAAAAA", "ABBBBBBBB", "BBCCCCCCC", "CCCDDDDDD", "DDDDX"},
	},
	{
		name:  "long url breaks into three lines at slashes",
		input: "https://example.com/api/v1/resources/12345/details/extra/path/parts/here/now",
		width: 30,
		want:  []string{"https://example.com/api/v1/", "resources/12345/details/", "extra/path/parts/here/now"},
	},
	{
		name:  "break char outside tolerance falls back to ideal",
		input: "x/" + strings.Repeat("y", 60),
		width: 20,
		want: []string{
			"x/" + strings.Repeat("y", 14),
			strings.Repeat("y", 16),
			strings.Repeat("y", 16),
			strings.Repeat("y", 14),
		},
	},
	{
		name:  "tight width forces many lines",
		input: "https://example.com/a/b/c/d/e",
		width: 6,
		want:  []string{"https:", "//exam", "ple.co", "m/a/b/", "c/d/e"},
	},
	{
		name:  "equidistant break chars pick the earlier one",
		input: "xxx/yyy/zzz",
		width: 8,
		want:  []string{"xxx/", "yyy/zzz"},
	},
	{
		name:  "dot in domain breaks earlier than slash if closer to ideal",
		input: "foo.bar.baz/qux/quux/corge",
		width: 12,
		want:  []string{"foo.bar.", "baz/qux/", "quux/corge"},
	},
}

func TestSmartColumnBreak(t *testing.T) {
	for _, tt := range smartColumnBreakCases {
		t.Run(tt.name, func(t *testing.T) {
			got := smartColumnBreak(tt.input, tt.width)
			require.Equal(t, tt.want, got)
			for _, ln := range got {
				require.LessOrEqual(t, len(ln), tt.width,
					"line %q exceeds width %d", ln, tt.width)
			}
			require.Equal(t, tt.input, strings.Join(got, ""),
				"joined lines reproduce input")
		})
	}
}

func TestSmartColumnBreakDeterministic(t *testing.T) {
	for _, tt := range smartColumnBreakCases {
		t.Run(tt.name, func(t *testing.T) {
			first := smartColumnBreak(tt.input, tt.width)
			for i := range 5 {
				again := smartColumnBreak(tt.input, tt.width)
				require.Equal(t, first, again,
					"run %d produced a different result for input %q",
					i+2, tt.input)
			}
		})
	}
}

func parseFirstValue(t *testing.T, src string) (*formatter, Expr) {
	t.Helper()
	wrapped := "k: " + src + "\n"
	f, err := ParseSource("t.ub", []byte(wrapped))
	require.NoError(t, err)
	return &formatter{comments: f.Comments, maxColumn: DefaultMaxColumn}, f.Body.Fields[0].Value
}

func TestSingleLineWidthAtoms(t *testing.T) {
	tests := []struct {
		name  string
		src   string
		width int
	}{
		{"number", "42", 2},
		{"negative number", "-7", 2},
		{"float", "3.14", 4},
		{"bool true", "true", 4},
		{"bool false", "false", 5},
		{"null", "null", 4},
		{"ident", "string", 6},
		{"single-quoted string", "'hi'", 4},
		{"triple-quote single line", "'''hi'''", 8},
		{"empty object", "{}", 2},
		{"empty array", "[]", 2},
		{"dot path", "input.x.y", 9},
		{"dot path with index", "input.x['k']", 12},
		{"bare call", "format('x', 1)", 14},
		{"library call", "lib.foo(1, 2)", 13},
		{"infix", "1 + 2", 5},
		{"prefix", "!input.x", 8},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, expr := parseFirstValue(t, tt.src)
			require.Equal(t, tt.width, w.singleLineWidth(expr))
		})
	}
}

func TestSingleLineWidthInlineCollections(t *testing.T) {
	w, expr := parseFirstValue(t, "{ a: 1, b: 'x' }")
	require.Equal(t, len("{ a: 1, b: 'x' }"), w.singleLineWidth(expr))

	w, expr = parseFirstValue(t, "[1, 2, 3]")
	require.Equal(t, len("[1, 2, 3]"), w.singleLineWidth(expr))
}

func TestSingleLineWidthMultilineStringForcesBreak(t *testing.T) {
	w, expr := parseFirstValue(t, "'''|\n  hi\n  '''")
	require.Equal(t, -1, w.singleLineWidth(expr))

	w, expr = parseFirstValue(t, "[1, '''|\n  hi\n  ''', 3]")
	require.Equal(t, -1, w.singleLineWidth(expr))

	w, expr = parseFirstValue(t, "{ a: 1, b: '''|\n  hi\n  ''' }")
	require.Equal(t, -1, w.singleLineWidth(expr))
}

func TestFitsOnLine(t *testing.T) {
	w, expr := parseFirstValue(t, "[1, 2, 3]")
	require.True(t, w.fitsOnLine(expr, 90))
	require.True(t, w.fitsOnLine(expr, 91))
	require.False(t, w.fitsOnLine(expr, 92))
}
