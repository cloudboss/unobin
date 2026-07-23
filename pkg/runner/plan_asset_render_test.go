package runner

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/asset"
)

func TestFormatValueDisplaysLogicalAssetReferences(t *testing.T) {
	key := base64.RawURLEncoding.EncodeToString([]byte("tree\x00main.go"))
	token := "unobin-asset:v1:path:" + strings.Repeat("0", 64) + ":" + key

	require.Equal(t, "<asset.tree['main.go'].path>", formatValue(token))
	require.Equal(
		t,
		"[<asset.tree['main.go'].path>]",
		formatValue([]any{asset.PathRef(token)}),
	)
	require.Equal(t, "'ordinary'", formatValue("ordinary"))
}
