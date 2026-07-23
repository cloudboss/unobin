package asset

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestContentSHA256(t *testing.T) {
	require.Equal(
		t,
		"e3b0c44298fc1c149afbf4c8996fb924"+
			"27ae41e4649b934ca495991b7852b855",
		contentSHA256(nil),
	)
	require.Equal(
		t,
		"ba7816bf8f01cfea414140de5dae2223"+
			"b00361a396177a9cb410ff61f20015ad",
		contentSHA256([]byte("abc")),
	)
}

func TestEntryIdentityUsesFramedFields(t *testing.T) {
	require.Equal(
		t,
		"ddd2dd6f9240517d965d239fefe4722e"+
			"33271b284b6a3c88212b9599e15f6eb3",
		entryIdentity(
			EntryKindFile,
			"0644",
			"ba7816bf8f01cfea414140de5dae2223"+
				"b00361a396177a9cb410ff61f20015ad",
		),
	)
	require.NotEqual(
		t,
		entryIdentity(EntryKind("ab"), "c", "d"),
		entryIdentity(EntryKind("a"), "bc", "d"),
	)
}

func TestAssetSetIDSortsAssetsWithoutChangingInput(t *testing.T) {
	assets := []CapturedAsset{
		capturedAssetWithRoot("second", strings.Repeat("b", 64)),
		capturedAssetWithRoot("first", strings.Repeat("a", 64)),
	}
	original := slices.Clone(assets)

	got, err := assetSetID(assets)

	require.NoError(t, err)
	require.Equal(
		t,
		"52d28cec8eef225b472f96330ffa0a16"+
			"d92c871bac2c9f60bba6f372ccdc8c18",
		got,
	)
	require.Equal(t, original, assets)
}

func TestAssetSetIDChangesWithNameOrRootIdentity(t *testing.T) {
	base, err := assetSetID([]CapturedAsset{
		capturedAssetWithRoot("program", strings.Repeat("a", 64)),
	})
	require.NoError(t, err)
	renamed, err := assetSetID([]CapturedAsset{
		capturedAssetWithRoot("renamed", strings.Repeat("a", 64)),
	})
	require.NoError(t, err)
	changed, err := assetSetID([]CapturedAsset{
		capturedAssetWithRoot("program", strings.Repeat("b", 64)),
	})
	require.NoError(t, err)

	require.NotEqual(t, base, renamed)
	require.NotEqual(t, base, changed)
}

func TestAssetSetIDRejectsInvalidAssets(t *testing.T) {
	tests := []struct {
		name   string
		assets []CapturedAsset
	}{
		{name: "empty set"},
		{
			name: "empty asset name",
			assets: []CapturedAsset{
				capturedAssetWithRoot("", strings.Repeat("a", 64)),
			},
		},
		{
			name: "duplicate name",
			assets: []CapturedAsset{
				capturedAssetWithRoot("program", strings.Repeat("a", 64)),
				capturedAssetWithRoot("program", strings.Repeat("b", 64)),
			},
		},
		{
			name: "missing root",
			assets: []CapturedAsset{{
				Name: "program",
				Entries: []CapturedEntry{{
					InternalPath:  "main",
					EntryIdentity: strings.Repeat("a", 64),
				}},
			}},
		},
		{
			name: "duplicate root",
			assets: []CapturedAsset{{
				Name: "program",
				Entries: []CapturedEntry{
					{EntryIdentity: strings.Repeat("a", 64)},
					{EntryIdentity: strings.Repeat("b", 64)},
				},
			}},
		},
		{
			name: "invalid root identity",
			assets: []CapturedAsset{{
				Name: "program",
				Entries: []CapturedEntry{{
					EntryIdentity: "ABC",
				}},
			}},
		},
		{
			name: "uppercase root identity",
			assets: []CapturedAsset{{
				Name: "program",
				Entries: []CapturedEntry{{
					EntryIdentity: strings.Repeat("A", 64),
				}},
			}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := assetSetID(tt.assets)
			require.Error(t, err)
		})
	}
}

func capturedAssetWithRoot(name, identity string) CapturedAsset {
	return CapturedAsset{
		Name: name,
		Entries: []CapturedEntry{{
			Kind:          EntryKindFile,
			Mode:          "0644",
			EntryIdentity: identity,
		}},
	}
}
