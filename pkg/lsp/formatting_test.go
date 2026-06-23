package lsp

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lsp/protocol"
)

func TestFormatTextFixtures(t *testing.T) {
	ubtest.Run(t, "testdata/ub/formatting/valid", func(name string, src []byte) (string, []string) {
		edits, rpcErr := FormatText(formattingFixturePath(name), string(src))
		if rpcErr != nil {
			return "", []string{rpcErr.Message}
		}
		return applyFormattingEdits(string(src), edits), nil
	}, ubtest.Idempotent())
}

func TestFormatTextReturnsWholeDocumentEdit(t *testing.T) {
	src := ubtest.ReadFixture(t, "testdata/ub/formatting/valid/factory-unformatted.ub")
	want := ubtest.ReadFixture(t, "testdata/ub/formatting/valid/factory-unformatted.ub.out")

	edits, rpcErr := FormatText("factory.ub", src)
	require.Nil(t, rpcErr)
	require.Len(t, edits, 1)
	require.Equal(t, protocol.Range{
		Start: protocol.Position{Line: 0, Character: 0},
		End:   OffsetToLSP(src, len(src)),
	}, edits[0].Range)
	require.Equal(t, want, edits[0].NewText)
}

func TestFormatTextReturnsNoEditsWhenFormatted(t *testing.T) {
	src := ubtest.ReadFixture(t, "testdata/ub/formatting/valid/factory-formatted.ub")

	edits, rpcErr := FormatText("factory.ub", src)
	require.Nil(t, rpcErr)
	require.Empty(t, edits)
}

func TestFormatTextRejectsInvalidUB(t *testing.T) {
	src := ubtest.ReadFixture(t, "testdata/ub/formatting/invalid/parse-error.ub")

	edits, rpcErr := FormatText("factory.ub", src)
	require.NotNil(t, rpcErr)
	require.Empty(t, edits)
}

func TestFormatTextMatchesLangFormatter(t *testing.T) {
	src := ubtest.ReadFixture(t, "testdata/ub/formatting/valid/factory-unformatted.ub")
	parsed, err := lang.ParseSource("factory.ub", []byte(src))
	require.NoError(t, err)
	want, err := lang.FormatWith(parsed, lang.FormatOptions{})
	require.NoError(t, err)

	edits, rpcErr := FormatText("factory.ub", src)
	require.Nil(t, rpcErr)
	require.Equal(t, string(want), applyFormattingEdits(src, edits))
}

func TestSessionFormattingReturnsDocumentEdit(t *testing.T) {
	session := NewSession("dev")
	src := ubtest.ReadFixture(t, "testdata/ub/formatting/valid/factory-unformatted.ub")
	want := ubtest.ReadFixture(t, "testdata/ub/formatting/valid/factory-unformatted.ub.out")

	rpcErr := openDocument(t, session, "file:///tmp/factory.ub", 1, src)
	require.Nil(t, rpcErr)
	result, rpcErr := requestFormatting(t, session, "file:///tmp/factory.ub")
	require.Nil(t, rpcErr)
	edits, ok := result.([]protocol.TextEdit)
	require.True(t, ok)
	require.Len(t, edits, 1)
	require.Equal(t, want, edits[0].NewText)
}

func requestFormatting(t *testing.T, session *Session, uri string) (any, *protocol.ResponseError) {
	t.Helper()
	params := protocol.DocumentFormattingParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
		Options:      protocol.FormattingOptions{TabSize: 2, InsertSpaces: true},
	}
	body, err := json.Marshal(params)
	require.NoError(t, err)
	return session.HandleRequest(context.Background(), &protocol.RequestMessage{
		JSONRPC: "2.0", Method: "textDocument/formatting", Params: body,
	})
}

func formattingFixturePath(name string) string {
	return filepath.Base(name) + ".ub"
}

func applyFormattingEdits(text string, edits []protocol.TextEdit) string {
	if len(edits) == 0 {
		return text
	}
	return edits[0].NewText
}
