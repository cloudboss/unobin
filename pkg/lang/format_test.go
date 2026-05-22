package lang

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func formatString(t *testing.T, src string) string {
	t.Helper()
	f, err := ParseSource("test.ub", []byte(src))
	require.NoError(t, err, "parse failed for source:\n%s", src)
	out, err := Format(f)
	require.NoError(t, err, "format failed")
	return string(out)
}

func TestFormatAtoms(t *testing.T) {
	src := `name:    'cfer'
port:    42
ratio:   1.5
enabled: true
empty:   null
`
	require.Equal(t, src, formatString(t, src))
}

func TestFormatNumberKeepsSourceText(t *testing.T) {
	src := `small:      42
fractional: 3.14
negative:   -7
`
	require.Equal(t, src, formatString(t, src))
}

func TestFormatNestedObject(t *testing.T) {
	src := `outer: {
  inner: 'x'
}
`
	require.Equal(t, src, formatString(t, src))
}

func TestFormatEmptyCollectionsInline(t *testing.T) {
	src := `obj:  {}
list: []
`
	require.Equal(t, src, formatString(t, src))
}

func TestFormatArray(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "empty_inline",
			src:  "items: []\n",
			want: "items: []\n",
		},
		{
			name: "empty_with_whitespace_normalized",
			src:  "items: [    ]\n",
			want: "items: []\n",
		},
		{
			name: "single_atom",
			src:  "items: [42]\n",
			want: "items: [42]\n",
		},
		{
			name: "short_atoms_inline",
			src:  "items: [1, 2, 3]\n",
			want: "items: [1, 2, 3]\n",
		},
		{
			name: "short_atoms_multi_line_collapses_to_inline",
			src:  "items: [\n  1,\n  2,\n  3,\n]\n",
			want: "items: [1, 2, 3]\n",
		},
		{
			name: "trailing_comma_dropped_in_inline_form",
			src:  "items: [1, 2, 3,]\n",
			want: "items: [1, 2, 3]\n",
		},
		{
			name: "messy_whitespace_normalized_inline",
			src:  "items:[ 1 , 2 , 3 ]\n",
			want: "items: [1, 2, 3]\n",
		},
		{
			name: "short_complex_inline",
			src:  "items: [core.file('a'), core.file('b')]\n",
			want: "items: [core.file('a'), core.file('b')]\n",
		},
		{
			name: "long_complex_per_line",
			src:  "items: [core.file('/very/long/path/to/some/important/file'), core.file('/another/very/long/path/to/different/file')]\n",
			want: "items: [\n  core.file('/very/long/path/to/some/important/file'),\n  core.file('/another/very/long/path/to/different/file'),\n]\n",
		},
		{
			name: "long_complex_multi_line_stays_per_line",
			src:  "items: [\n  core.file('/very/long/path/to/some/important/file'),\n  core.file('/another/very/long/path/to/different/file'),\n]\n",
			want: "items: [\n  core.file('/very/long/path/to/some/important/file'),\n  core.file('/another/very/long/path/to/different/file'),\n]\n",
		},
		{
			name: "long_atoms_balanced_packed",
			src:  "items: ['aaaa', 'bbbb', 'cccc', 'dddd', 'eeee', 'ffff', 'gggg', 'hhhh', 'iiii', 'jjjj', 'kkkk', 'llll', 'mmmm', 'nnnn']\n",
			want: "items: [\n  'aaaa', 'bbbb', 'cccc', 'dddd', 'eeee', 'ffff', 'gggg',\n  'hhhh', 'iiii', 'jjjj', 'kkkk', 'llll', 'mmmm', 'nnnn',\n]\n",
		},
		{
			name: "long_atoms_repacked_from_one_per_line",
			src:  "items: [\n  'aaaa',\n  'bbbb',\n  'cccc',\n  'dddd',\n  'eeee',\n  'ffff',\n  'gggg',\n  'hhhh',\n  'iiii',\n  'jjjj',\n  'kkkk',\n  'llll',\n  'mmmm',\n  'nnnn',\n]\n",
			want: "items: [\n  'aaaa', 'bbbb', 'cccc', 'dddd', 'eeee', 'ffff', 'gggg',\n  'hhhh', 'iiii', 'jjjj', 'kkkk', 'llll', 'mmmm', 'nnnn',\n]\n",
		},
		{
			name: "mixed_atoms_and_complex_per_line",
			src:  "items: [1, core.file('/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'), 2]\n",
			want: "items: [\n  1,\n  core.file('/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'),\n  2,\n]\n",
		},
		{
			name: "sub_arrays_per_line",
			src:  "items: [\n  [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17],\n  [18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32],\n]\n",
			want: "items: [\n  [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17],\n  [18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32],\n]\n",
		},
		{
			name: "dot_paths_per_line",
			src:  "items: [\n  var.alpha.beta.gamma.delta,\n  var.epsilon.zeta.eta.theta.iota.kappa.lambda.mu.nu.xi.omicron.pi.rho.sigma,\n]\n",
			want: "items: [\n  var.alpha.beta.gamma.delta,\n  var.epsilon.zeta.eta.theta.iota.kappa.lambda.mu.nu.xi.omicron.pi.rho.sigma,\n]\n",
		},
		{
			name: "comment_between_atoms_forces_per_line",
			src:  "items: [\n  1,\n  # an aside\n  2,\n  3,\n]\n",
			want: "items: [\n  1,\n  # an aside\n  2,\n  3,\n]\n",
		},
		{
			name: "comment_before_first_element",
			src:  "items: [\n  # leading\n  1,\n  2,\n  3,\n]\n",
			want: "items: [\n  # leading\n  1,\n  2,\n  3,\n]\n",
		},
		{
			name: "comment_after_last_element",
			src:  "items: [\n  1,\n  2,\n  3,\n  # trailing\n]\n",
			want: "items: [\n  1,\n  2,\n  3,\n  # trailing\n]\n",
		},
		{
			name: "multiline_string_element_forces_per_line",
			src:  "items: [\n  1,\n  `|\n    hello\n    `,\n  3,\n]\n",
			want: "items: [\n  1,\n  `|\n    hello\n    `,\n  3,\n]\n",
		},
		{
			name: "bool_atoms_inline",
			src:  "items: [true, false, null]\n",
			want: "items: [true, false, null]\n",
		},
		{
			name: "ident_atoms_inline",
			src:  "items: [string, integer, boolean]\n",
			want: "items: [string, integer, boolean]\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatString(t, tt.src)
			require.Equal(t, tt.want, got)
			again := formatString(t, got)
			require.Equal(t, got, again, "format is not idempotent")
		})
	}
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
			name:      "folded_backtick_respects_tight_max",
			maxColumn: 20,
			src:       "msg: `>\n  one two three four five six seven eight nine ten\n  `\n",
			want:      "msg: `>\n  one two three four\n  five six seven\n  eight nine ten\n  `\n",
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

func TestFormatArrayDeterministic(t *testing.T) {
	tests := []string{
		"items: [1, 2, 3]\n",
		"items: ['aaaa', 'bbbb', 'cccc', 'dddd', 'eeee', 'ffff', 'gggg', 'hhhh', 'iiii', 'jjjj', 'kkkk', 'llll', 'mmmm', 'nnnn']\n",
		"items: [\n  core.file('/very/long/path/to/some/important/file'),\n  core.file('/another/very/long/path/to/different/file'),\n]\n",
		"items: [\n  1,\n  # comment\n  2,\n]\n",
	}
	for _, src := range tests {
		first := formatString(t, src)
		for i := 0; i < 5; i++ {
			again := formatString(t, src)
			require.Equal(t, first, again, "iteration %d differs", i)
		}
	}
}

func TestFormatMetaKeyStaysBare(t *testing.T) {
	src := `@trigger:  'x'
@for-each: var.items
plain:     'y'
`
	require.Equal(t, src, formatString(t, src))
}

func TestFormatPreservesQuotedKey(t *testing.T) {
	src := `'has space': 1
plain:       2
`
	require.Equal(t, src, formatString(t, src))
}

func TestFormatDotPath(t *testing.T) {
	src := `a: var.region
b: resource.local.file.x.path
c: var.cfg['key']
`
	require.Equal(t, src, formatString(t, src))
}

func TestFormatCall(t *testing.T) {
	src := `a: lib.foo(var.x, var.y)
b: range(1, 5)
`
	require.Equal(t, src, formatString(t, src))
}

func TestFormatInfixAndPrefix(t *testing.T) {
	src := `a: 1 + 2
b: !var.flag
c: var.x == 'y'
`
	require.Equal(t, src, formatString(t, src))
}

func TestFormatTypeExpressions(t *testing.T) {
	src := `inputs: {
  region: {
    type: string
  }
  ports: {
    type: list(integer)
  }
  cfg: {
    type: optional(map(string))
  }
}
`
	require.Equal(t, src, formatString(t, src))
}

func TestFormatLeadingComment(t *testing.T) {
	src := `# top
name: 'x'
`
	require.Equal(t, src, formatString(t, src))
}

func TestFormatTrailingComment(t *testing.T) {
	src := `name: 'x'  # tail
`
	require.Equal(t, src, formatString(t, src))
}

func TestFormatCommentBetweenSiblings(t *testing.T) {
	src := `a: 1
# divider
b: 2
`
	require.Equal(t, src, formatString(t, src))
}

func TestFormatCommentInsideObject(t *testing.T) {
	src := `outer: {
  a: 1
  # divider
  b: 2
}
`
	require.Equal(t, src, formatString(t, src))
}

func TestFormatCommentAfterLastFieldOfObject(t *testing.T) {
	src := `outer: {
  a: 1
  # trailing inside
}
after: 'x'
`
	require.Equal(t, src, formatString(t, src))
}

func TestFormatCommentAfterCloseBrace(t *testing.T) {
	src := `outer: {
  a: 1
}
# after
b: 2
`
	require.Equal(t, src, formatString(t, src))
}

func TestFormatBlankLineBetweenSiblings(t *testing.T) {
	src := `a: 1

b: 2
`
	require.Equal(t, src, formatString(t, src))
}

func TestFormatCollapsesMultipleBlankLines(t *testing.T) {
	src := `a: 1



b: 2
`
	want := `a: 1

b: 2
`
	require.Equal(t, want, formatString(t, src))
}

func TestFormatMultilineString(t *testing.T) {
	src := "script: `|\n  echo hi\n  echo bye\n  `\n"
	require.Equal(t, src, formatString(t, src))
}

func TestFormatMultilineStringNoSpuriousBlankBefore(t *testing.T) {
	src := "script: `|\n  one\n  two\n  `\nnext: 'x'\n"
	require.Equal(t, src, formatString(t, src))
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
			for i := 0; i < 5; i++ {
				again := smartColumnBreak(tt.input, tt.width)
				require.Equal(t, first, again,
					"run %d produced a different result for input %q",
					i+2, tt.input)
			}
		})
	}
}

func TestFormatJoinedWrapsLongValue(t *testing.T) {
	value := "https://very-long-domain.example.com/" +
		strings.Repeat("api/v1/resources/", 5) + "details"
	src := "k: `\\-\n  " + value + "\n  `\n"
	formatted := formatString(t, src)

	require.Greater(t, strings.Count(formatted, "\n"), 3,
		"expected multi-line output, got:\n%s", formatted)

	for _, line := range strings.Split(formatted, "\n") {
		require.LessOrEqual(t, len(line), 100,
			"line exceeds 100 columns: %q", line)
	}

	f, err := ParseSource("test.ub", []byte(formatted))
	require.NoError(t, err)
	got := f.Body.Fields[0].Value.(*StringLit).Value
	require.Equal(t, value, got)
}

func TestFormatBacktickAllSigils(t *testing.T) {
	tests := []struct {
		name string
		src  string
	}{
		{
			name: "literal clip",
			src:  "k: `|\n  one\n  two\n  `\n",
		},
		{
			name: "literal strip",
			src:  "k: `|-\n  one\n  two\n  `\n",
		},
		{
			name: "folded clip",
			src:  "k: `>\n  one two\n  `\n",
		},
		{
			name: "folded strip with paragraphs",
			src:  "k: `>-\n  paragraph one\n\n  paragraph two\n  `\n",
		},
		{
			name: "joined clip url",
			src:  "k: `\\\n  https://example.com/api/v1/users\n  `\n",
		},
		{
			name: "joined strip arn",
			src:  "k: `\\-\n  arn:aws:s3:::bucket/key\n  `\n",
		},
		{
			name: "single-line backtick",
			src:  "k: `it's here`\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.src, formatString(t, tt.src))
		})
	}
}

func TestFormatAlignsValuesAcrossKeyLengths(t *testing.T) {
	in := `a: 1
bb: 2
ccc: 3
`
	want := `a:   1
bb:  2
ccc: 3
`
	require.Equal(t, want, formatString(t, in))
}

func TestFormatBlankLineBreaksAlignmentGroup(t *testing.T) {
	in := `a: 1
bb: 2

ccc: 3
`
	want := `a:  1
bb: 2

ccc: 3
`
	require.Equal(t, want, formatString(t, in))
}

func TestFormatMultilineFieldBreaksAlignmentGroup(t *testing.T) {
	in := `aa: 1
bbbbbb: {
  x: 1
}
cc: 2
`
	want := `aa: 1
bbbbbb: {
  x: 1
}
cc: 2
`
	require.Equal(t, want, formatString(t, in))
}

func TestFormatCommentInsideGroupKeepsAlignment(t *testing.T) {
	in := `a: 1
# comment between
bbbb: 2
`
	want := `a:    1
# comment between
bbbb: 2
`
	require.Equal(t, want, formatString(t, in))
}

func TestFormatAlignsInsideNestedObject(t *testing.T) {
	in := `inputs: {
  a: 1
  bbbb: 2
}
`
	want := `inputs: {
  a:    1
  bbbb: 2
}
`
	require.Equal(t, want, formatString(t, in))
}

func TestFormatTrailingCommentNotAligned(t *testing.T) {
	in := `a: 1  # x
bb: 22  # y
ccc: 333  # z
`
	want := `a:   1  # x
bb:  22  # y
ccc: 333  # z
`
	require.Equal(t, want, formatString(t, in))
}

func TestFormatAlignsAcrossDeepNesting(t *testing.T) {
	in := `top-a: 1
top-bbb: 2
top-cccc: {
  mid-a: 1
  mid-bbb: 2
  mid-cccc: {
    inner-a: 1
    inner-bbbbb: 2
  }
}
`
	want := `top-a:   1
top-bbb: 2
top-cccc: {
  mid-a:   1
  mid-bbb: 2
  mid-cccc: {
    inner-a:     1
    inner-bbbbb: 2
  }
}
`
	require.Equal(t, want, formatString(t, in))
}

func TestFormatParallelNestedObjectsAlignIndependently(t *testing.T) {
	in := `left: {
  a: 1
  bbb: 2
}
right: {
  xxxxxx: 1
  y: 2
}
`
	want := `left: {
  a:   1
  bbb: 2
}
right: {
  xxxxxx: 1
  y:      2
}
`
	require.Equal(t, want, formatString(t, in))
}

func TestFormatAlignsMixedValueTypes(t *testing.T) {
	in := `str: 'x'
num: 42
fp: 1.5
flag: true
empty: null
path: var.x
call: range(1, 5)
sum: 1 + 2
neg: !var.flag
`
	want := `str:   'x'
num:   42
fp:    1.5
flag:  true
empty: null
path:  var.x
call:  range(1, 5)
sum:   1 + 2
neg:   !var.flag
`
	require.Equal(t, want, formatString(t, in))
}

func TestFormatAlignsAroundEmptyCollections(t *testing.T) {
	in := `obj: {}
list: []
str: 'x'
`
	want := `obj:  {}
list: []
str:  'x'
`
	require.Equal(t, want, formatString(t, in))
}

func TestFormatGroupResumesAfterMultilineValue(t *testing.T) {
	in := `a: 1
bb: 2
ccc: {
  x: 1
}
dd: 3
eeeee: 4
`
	want := `a:  1
bb: 2
ccc: {
  x: 1
}
dd:    3
eeeee: 4
`
	require.Equal(t, want, formatString(t, in))
}

func TestFormatBlankAfterCommentBreaksGroup(t *testing.T) {
	in := `a: 1
# divider

bbb: 2
`
	want := `a: 1
# divider

bbb: 2
`
	require.Equal(t, want, formatString(t, in))
}

func TestFormatTopLevelMixOfSingleAndMultiline(t *testing.T) {
	in := `description: 'demo'
imports: {
  core: 'foo'
  local: 'bar'
}
name: 'x'
version: 'v1'
`
	want := `description: 'demo'
imports: {
  core:  'foo'
  local: 'bar'
}
name:    'x'
version: 'v1'
`
	require.Equal(t, want, formatString(t, in))
}

func TestFormatMetaKeyParticipatesInAlignment(t *testing.T) {
	in := `@trigger: 'x'
name: 'y'
`
	want := `@trigger: 'x'
name:     'y'
`
	require.Equal(t, want, formatString(t, in))
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
		{"backtick single line", "`hi`", 4},
		{"empty object", "{}", 2},
		{"empty array", "[]", 2},
		{"dot path", "var.x.y", 7},
		{"dot path with index", "var.x['k']", 10},
		{"bare call", "format('x', 1)", 14},
		{"module call", "lib.foo(1, 2)", 13},
		{"infix", "1 + 2", 5},
		{"prefix", "!var.x", 6},
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
	w, expr := parseFirstValue(t, "`|\n  hi\n  `")
	require.Equal(t, -1, w.singleLineWidth(expr))

	w, expr = parseFirstValue(t, "[1, `|\n  hi\n  `, 3]")
	require.Equal(t, -1, w.singleLineWidth(expr))

	w, expr = parseFirstValue(t, "{ a: 1, b: `|\n  hi\n  ` }")
	require.Equal(t, -1, w.singleLineWidth(expr))
}

func TestSingleLineWidthCommentInsideCollectionForcesBreak(t *testing.T) {
	src := "k: {\n  a: 1\n  # nope\n  b: 2\n}\n"
	f, err := ParseSource("t.ub", []byte(src))
	require.NoError(t, err)
	w := &formatter{comments: f.Comments}
	require.Equal(t, -1, w.singleLineWidth(f.Body.Fields[0].Value))
}

func TestSingleLineWidthPromotedTypeExpressions(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want int
	}{
		{"atomic", "string", len("string")},
		{"list", "list(string)", len("list(string)")},
		{"optional", "optional(map(string))", len("optional(map(string))")},
		{"empty type object", "object({})", len("object({})")},
		{"non-empty type object forces break", "object({ a: integer })", -1},
		{"optional with default", "optional(integer, 3)", len("optional(integer, 3)")},
		{"tuple", "tuple([string, integer])", len("tuple([string, integer])")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, expr := parseFirstValue(t, tt.src)
			te, err := PromoteType(expr)
			require.NoError(t, err)
			require.Equal(t, tt.want, w.singleLineWidth(te))
		})
	}
}

func TestFitsOnLine(t *testing.T) {
	w, expr := parseFirstValue(t, "[1, 2, 3]")
	require.True(t, w.fitsOnLine(expr, 90))
	require.True(t, w.fitsOnLine(expr, 91))
	require.False(t, w.fitsOnLine(expr, 92))
}

func TestFormatIdempotent(t *testing.T) {
	src := `# top
output: {
  region: 'us-east-1'
  # nested comment
  items:  [1, 2]
}

other: var.x.y
`
	once := formatString(t, src)
	require.Equal(t, src, once)
	twice := formatString(t, once)
	require.Equal(t, once, twice)
}
