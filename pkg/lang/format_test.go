package lang

import (
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

func TestFormatArrayHasTrailingCommas(t *testing.T) {
	src := `items: [
  1,
  2,
  3,
]
`
	require.Equal(t, src, formatString(t, src))
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

func TestFormatIdempotent(t *testing.T) {
	src := `# top
output: {
  region: 'us-east-1'
  # nested comment
  items: [
    1,
    2,
  ]
}

other: var.x.y
`
	once := formatString(t, src)
	require.Equal(t, src, once)
	twice := formatString(t, once)
	require.Equal(t, once, twice)
}
