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
	src := `name: 'cfer'
port: 42
ratio: 1.5
enabled: true
empty: null
`
	require.Equal(t, src, formatString(t, src))
}

func TestFormatNumberKeepsSourceText(t *testing.T) {
	src := `small: 42
fractional: 3.14
negative: -7
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
	src := `obj: {}
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

func TestFormatPreservesQuotedKey(t *testing.T) {
	src := `'has space': 1
plain: 2
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
	src := "script: `\n  echo hi\n  echo bye\n`\n"
	require.Equal(t, src, formatString(t, src))
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
