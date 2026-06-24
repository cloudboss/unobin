package lsp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/deps"
	"github.com/cloudboss/unobin/pkg/lsp/protocol"
	"github.com/cloudboss/unobin/pkg/resolve"
)

func TestCompletionFileRolesAtEmptySource(t *testing.T) {
	root := writeUBProject(t, nil, nil)
	path := filepath.Join(root, "factory.ub")

	list, rpcErr := CompleteForText(path, "", protocol.Position{}, NewProjectCache(root))
	require.Nil(t, rpcErr)
	requireCompletionLabels(t, list, "factory", "stack", "project", "project-lock")
}

func TestCompletionFactoryBlocks(t *testing.T) {
	root := writeUBProject(t, nil, nil)
	path := filepath.Join(root, "factory.ub")
	source := ubtest.ReadValidFixture(t, "testdata/ub/completion", "factory-empty-block")
	offset := strings.Index(source, "\n\n") + 1
	pos := OffsetToLSP(source, offset)

	list, rpcErr := CompleteForText(path, source, pos, NewProjectCache(root))
	require.Nil(t, rpcErr)
	requireCompletionLabels(t, list, "inputs", "resources", "outputs", "constraints")
}

func TestCompletionStackBlocks(t *testing.T) {
	root := writeUBProject(t, nil, nil)
	path := filepath.Join(root, "stack.ub")
	source := ubtest.ReadValidFixture(t, "testdata/ub/completion", "stack-empty-block")
	offset := strings.Index(source, "\n\n") + 1
	pos := OffsetToLSP(source, offset)

	list, rpcErr := CompleteForText(path, source, pos, NewProjectCache(root))
	require.Nil(t, rpcErr)
	requireCompletionLabels(t, list, "factory", "state", "encryption", "locals", "parallelism")
}

func TestCompletionStackStateSelectors(t *testing.T) {
	root := writeUBProject(t, nil, nil)
	path := filepath.Join(root, "stack.ub")
	source := ubtest.ReadValidFixture(t, "testdata/ub/completion", "stack-state-selector")
	pos := positionInText(source, "state: local", "local")

	list, rpcErr := CompleteForText(path, source, pos, NewProjectCache(root))
	require.Nil(t, rpcErr)
	requireCompletionLabels(t, list, "local", "s3")
}

func TestCompletionStackEncryptionSelectors(t *testing.T) {
	root := writeUBProject(t, nil, nil)
	path := filepath.Join(root, "stack.ub")
	source := ubtest.ReadValidFixture(t, "testdata/ub/completion", "stack-encryption-selector")
	pos := positionInText(source, "encryption: noop", "noop")

	list, rpcErr := CompleteForText(path, source, pos, NewProjectCache(root))
	require.Nil(t, rpcErr)
	requireCompletionLabels(t, list, "env-key", "kms", "noop")
}

func TestCompletionProjectKeys(t *testing.T) {
	root := writeUBProject(t, nil, nil)
	path := filepath.Join(root, "project.ub")
	source := ubtest.ReadValidFixture(t, "testdata/ub/completion", "project-empty-block")
	offset := strings.Index(source, "\n\n") + 1
	pos := OffsetToLSP(source, offset)

	list, rpcErr := CompleteForText(path, source, pos, NewProjectCache(root))
	require.Nil(t, rpcErr)
	requireCompletionLabels(t, list, "unobin-version", "requires", "replace")
}

func TestCompletionProjectRequirementKeys(t *testing.T) {
	root := writeUBProject(t, nil, nil)
	path := filepath.Join(root, "project.ub")
	source := ubtest.ReadValidFixture(t, "testdata/ub/completion", "project-requirement")
	pos := positionInText(source, "version: 'v1.0.0'", "version")

	list, rpcErr := CompleteForText(path, source, pos, NewProjectCache(root))
	require.Nil(t, rpcErr)
	requireCompletionLabels(t, list, "version", "indirect")
}

func TestCompletionInputDeclarationKeys(t *testing.T) {
	root, path, source := completionProject(t)
	pos := positionInText(source, "type: string", "type")

	list, rpcErr := CompleteForText(path, source, pos, NewProjectCache(root))
	require.Nil(t, rpcErr)
	requireCompletionLabels(t, list, "type", "description", "default")
}

func TestCompletionInputDeclarationMetaKeys(t *testing.T) {
	root, path, source := completionProject(t)
	source, pos := sourceWithCompletionCursor(
		t, source, "description: 'AWS region to use.'", "@",
	)

	list, rpcErr := CompleteForText(path, source, pos, NewProjectCache(root))
	require.Nil(t, rpcErr)
	requireCompletionLabels(t, list, "@sensitive")
	requireNotCompletionLabels(t, list, "default", "description", "sensitive", "type")
}

func TestCompletionInputMetaValueUsesExpectedType(t *testing.T) {
	root, path, source := completionProject(t)
	source, pos := sourceWithCompletionCursor(
		t, source, "description: 'AWS region to use.'", "@sensitive: ",
	)

	list, rpcErr := CompleteForText(path, source, pos, NewProjectCache(root))
	require.Nil(t, rpcErr)
	requireOnlyCompletionLabels(t, list, "false", "true")
}

func TestCompletionInputDefaultUsesDeclaredType(t *testing.T) {
	root, path, source := completionProject(t)
	source, pos := sourceWithCompletionCursor(
		t, source, "type: integer", "type: boolean default: ",
	)

	list, rpcErr := CompleteForText(path, source, pos, NewProjectCache(root))
	require.Nil(t, rpcErr)
	requireOnlyCompletionLabels(t, list, "false", "true")
}

func TestCompletionInlineInputDeclarationFieldsExcludeRoots(t *testing.T) {
	root, path, source := inputDeclarationCompletionProject(t)
	tests := []struct {
		name     string
		inserted string
		want     []string
		notWant  []string
	}{
		{
			name:     "empty prefix",
			inserted: "",
			want:     []string{"@sensitive", "default", "description"},
			notWant:  []string{"@core", "action", "input", "local", "resource", "type"},
		},
		{
			name:     "meta key",
			inserted: "@",
			want:     []string{"@sensitive"},
			notWant:  []string{"@core", "default", "description", "type"},
		},
		{
			name:     "description prefix",
			inserted: "d",
			want:     []string{"default", "description"},
			notWant:  []string{"@core", "action", "input", "local", "resource", "type"},
		},
		{
			name:     "unknown prefix",
			inserted: "h",
			notWant:  []string{"@core", "action", "input", "local", "resource"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := inlineInputDeclarationSourceWithPrefix(t, source, tt.inserted)
			list, rpcErr := CompleteForText(path, source, pos, NewProjectCache(root))
			require.Nil(t, rpcErr)
			requireCompletionLabels(t, list, tt.want...)
			requireNotCompletionLabels(t, list, tt.notWant...)
		})
	}
}

func TestCompletionInputDeclarationFieldsExcludePresentKeys(t *testing.T) {
	root, path, source := inputDeclarationCompletionProject(t)
	tests := []struct {
		name      string
		inserted  string
		want      []string
		notWant   []string
		wantEmpty bool
	}{
		{
			name:     "meta key",
			inserted: "@",
			want:     []string{"@sensitive"},
			notWant:  []string{"default", "description", "sensitive", "type"},
		},
		{
			name:      "unknown prefix",
			inserted:  "a",
			notWant:   []string{"@core", "action", "input", "local", "resource"},
			wantEmpty: true,
		},
		{
			name:     "description prefix",
			inserted: "d",
			want:     []string{"description"},
			notWant:  []string{"default", "type"},
		},
		{
			name:      "present type prefix",
			inserted:  "t",
			notWant:   []string{"@core", "action", "input", "local", "resource", "type"},
			wantEmpty: true,
		},
		{
			name:      "input root prefix",
			inserted:  "i",
			notWant:   []string{"input"},
			wantEmpty: true,
		},
		{
			name:      "local root prefix",
			inserted:  "l",
			notWant:   []string{"local"},
			wantEmpty: true,
		},
		{
			name:      "resource root prefix",
			inserted:  "r",
			notWant:   []string{"resource"},
			wantEmpty: true,
		},
		{
			name:      "data source root prefix",
			inserted:  "s",
			notWant:   []string{"@sensitive", "data-source"},
			wantEmpty: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := inputDeclarationSourceWithPrefix(t, source, tt.inserted)
			list, rpcErr := CompleteForText(path, source, pos, NewProjectCache(root))
			require.Nil(t, rpcErr)
			if tt.wantEmpty {
				require.Empty(t, list.Items)
			}
			requireCompletionLabels(t, list, tt.want...)
			requireNotCompletionLabels(t, list, tt.notWant...)
		})
	}
}

func TestCompletionTypeConstructors(t *testing.T) {
	root, path, source := completionProject(t)
	source, pos := sourceWithCompletionCursor(t, source, "type: string", "type: ")

	list, rpcErr := CompleteForText(path, source, pos, NewProjectCache(root))
	require.Nil(t, rpcErr)
	requireCompletionLabels(t, list, "string", "integer", "list", "object", "optional")
}

func TestCompletionResourcesSelectorUsesResourceSchemaOnly(t *testing.T) {
	root, path, source, _ := goDefinitionProject(t)
	source, pos := sourceWithCompletionCursor(t, source, "def.server", "def.")

	list, rpcErr := CompleteForText(path, source, pos, NewProjectCache(root))
	require.Nil(t, rpcErr)
	requireCompletionLabels(t, list, "server")
	requireNotCompletionLabels(t, list, "lookup", "deploy", "slug")
}

func TestCompletionGoBackedBodyFieldsAtBlankPosition(t *testing.T) {
	root, path, source, _ := goDefinitionProject(t)
	source = strings.Replace(source, "server-name: 'web'", "", 1)
	settingsOffset := strings.Index(source, "      settings:")
	require.NotEqual(t, -1, settingsOffset)
	source = source[:settingsOffset] + "      \n" + source[settingsOffset:]
	pos := OffsetToLSP(source, settingsOffset+6)

	list, rpcErr := CompleteForText(path, source, pos, NewProjectCache(root))
	require.Nil(t, rpcErr)
	requireCompletionLabels(t, list, "server-name")
	requireNotCompletionLabels(t, list, "id", "settings")
}

func TestCompletionResourceBodyMetaKeys(t *testing.T) {
	root, path, source, _ := goDefinitionProject(t)
	source, pos := sourceWithCompletionCursor(t, source, "server-name: 'web'", "@")

	list, rpcErr := CompleteForText(path, source, pos, NewProjectCache(root))
	require.Nil(t, rpcErr)
	requireCompletionLabels(t, list, "@depends-on", "@for-each", "@lock", "@timeout")
	requireNotCompletionLabels(t, list, "@trigger")
}

func TestCompletionResourceBodyMetaKeysReplaceTypedAt(t *testing.T) {
	root, path, source, _ := goDefinitionProject(t)
	source, pos := sourceWithCompletionCursor(t, source, "server-name: 'web'", "@")

	list, rpcErr := CompleteForText(path, source, pos, NewProjectCache(root))
	require.Nil(t, rpcErr)
	item := requireCompletionItem(t, list, "@for-each")
	require.Equal(t, "@for-each", item.FilterText)
	require.Equal(t, "@for-each", applyCompletionTextEdit(t, source, item, "@"))
}

func TestCompletionResourceBodyMetaKeysKeepTypedPrefix(t *testing.T) {
	root, path, source, _ := goDefinitionProject(t)
	source, pos := sourceWithCompletionCursor(t, source, "server-name: 'web'", "@f")

	list, rpcErr := CompleteForText(path, source, pos, NewProjectCache(root))
	require.Nil(t, rpcErr)
	item := requireCompletionItem(t, list, "@for-each")
	require.Equal(t, "@for-each", item.FilterText)
	require.Equal(t, "@for-each", applyCompletionTextEdit(t, source, item, "@f"))
}

func TestCompletionActionBodyMetaKeys(t *testing.T) {
	root, path, source, _ := goDefinitionProject(t)
	source, pos := sourceWithCompletionCursor(t, source, "version: def.slug('v1')", "@")

	list, rpcErr := CompleteForText(path, source, pos, NewProjectCache(root))
	require.Nil(t, rpcErr)
	requireCompletionLabels(t, list, "@depends-on", "@for-each", "@lock", "@timeout", "@trigger")
}

func TestCompletionGoBackedBodyKeyPrefixUsesSchemaContext(t *testing.T) {
	root, path, _, _ := goDefinitionProject(t)
	source := ubtest.ReadValidFixture(
		t, "testdata/ub/completion", "go-backed-typed-value",
	)
	source, pos := sourceWithCompletionCursor(t, source, "server-name: 'web'", "s")

	list, rpcErr := CompleteForText(path, source, pos, NewProjectCache(root))
	require.Nil(t, rpcErr)
	requireCompletionLabels(t, list, "server-name")
	requireNotCompletionLabels(t, list,
		"@core", "action", "data-source", "input", "local", "resource", "settings")
}

func TestCompletionGoBackedBodyKeyPrefixExcludesPresentFields(t *testing.T) {
	root, path, _, _ := goDefinitionProject(t)
	source := ubtest.ReadValidFixture(
		t, "testdata/ub/completion", "go-backed-typed-value",
	)
	source, pos := sourceWithCompletionCursor(t, source, "id: 'input-id'", "s")

	list, rpcErr := CompleteForText(path, source, pos, NewProjectCache(root))
	require.Nil(t, rpcErr)
	require.Empty(t, list.Items)
}

func TestCompletionGoBackedStringFieldValueUsesTypeContext(t *testing.T) {
	root, path, _, _ := goDefinitionProject(t)
	source := ubtest.ReadValidFixture(
		t, "testdata/ub/completion", "go-backed-typed-value",
	)
	source, pos := sourceWithCompletionCursor(t, source, "server-name: 'web'", "server-name: ")

	list, rpcErr := CompleteForText(path, source, pos, NewProjectCache(root))
	require.Nil(t, rpcErr)
	requireCompletionLabels(t, list, "input.name", "local.std-name")
	requireNotCompletionLabels(t, list,
		"@core", "action", "data-source", "input", "local", "resource",
		"input.count", "input.definition-config", "local.tags")
}

func TestCompletionGoBackedFieldValueDoesNotFallBackToRoots(t *testing.T) {
	root, path, source, _ := goDefinitionProject(t)
	source, pos := sourceWithCompletionCursor(t, source, "id: 'input-id'", "id: ")

	list, rpcErr := CompleteForText(path, source, pos, NewProjectCache(root))
	require.Nil(t, rpcErr)
	require.Empty(t, list.Items)
}

func TestCompletionOutputMetaKeys(t *testing.T) {
	root, path, source := completionProject(t)
	source, pos := sourceWithCompletionCursor(t, source, "value: null", "@")

	list, rpcErr := CompleteForText(path, source, pos, NewProjectCache(root))
	require.Nil(t, rpcErr)
	requireCompletionLabels(t, list, "@sensitive")
	requireNotCompletionLabels(t, list, "sensitive")
}

func TestCompletionOutputMetaValueUsesExpectedType(t *testing.T) {
	root, path, source := completionProject(t)
	source, pos := sourceWithCompletionCursor(
		t, source, "value: null", "value: null @sensitive: ",
	)

	list, rpcErr := CompleteForText(path, source, pos, NewProjectCache(root))
	require.Nil(t, rpcErr)
	requireOnlyCompletionLabels(t, list, "false", "true")
}

func TestCompletionRoots(t *testing.T) {
	root, path, source := completionProject(t)
	source, pos := sourceWithCompletionCursor(t, source, "null", "")

	list, rpcErr := CompleteForText(path, source, pos, NewProjectCache(root))
	require.Nil(t, rpcErr)
	requireCompletionLabels(t, list,
		"input", "local", "resource", "data-source", "action", "@core")
}

func TestCompletionLocalNames(t *testing.T) {
	root, path, source := completionProject(t)
	cache := NewProjectCache(root)

	inputSource, inputPos := sourceWithCompletionCursor(t, source, "input.region", "input.")
	list, rpcErr := CompleteForText(path, inputSource, inputPos, cache)
	require.Nil(t, rpcErr)
	requireCompletionLabels(t, list, "region", "count")

	localSource, localPos := sourceWithCompletionCursor(t, source, "local.name", "local.")
	list, rpcErr = CompleteForText(path, localSource, localPos, cache)
	require.Nil(t, rpcErr)
	requireCompletionLabels(t, list, "name")
}

func TestCompletionNodeNames(t *testing.T) {
	root, path, source := completionProject(t)
	cache := NewProjectCache(root)

	resourceSource, resourcePos := sourceWithCompletionCursor(
		t, source, "resource.server", "resource.",
	)
	list, rpcErr := CompleteForText(path, resourceSource, resourcePos, cache)
	require.Nil(t, rpcErr)
	requireCompletionLabels(t, list, "server")

	dataSource, dataPos := sourceWithCompletionCursor(
		t, source, "data-source.lookup", "data-source.",
	)
	list, rpcErr = CompleteForText(path, dataSource, dataPos, cache)
	require.Nil(t, rpcErr)
	requireCompletionLabels(t, list, "lookup")

	actionSource, actionPos := sourceWithCompletionCursor(
		t, source, "action.deploy", "action.",
	)
	list, rpcErr = CompleteForText(path, actionSource, actionPos, cache)
	require.Nil(t, rpcErr)
	requireCompletionLabels(t, list, "deploy")
}

func TestCompletionGoBackedSelectors(t *testing.T) {
	root, path, source, _ := goDefinitionProject(t)
	source, pos := sourceWithCompletionCursor(t, source, "def.server", "def.")

	list, rpcErr := CompleteForText(path, source, pos, NewProjectCache(root))
	require.Nil(t, rpcErr)
	requireCompletionLabels(t, list, "server")
	requireNotCompletionLabels(t, list, "lookup", "deploy", "slug")
}

func TestCompletionUBCompositeSelectors(t *testing.T) {
	root, path, source, _ := definitionProject(t)
	source, pos := sourceWithCompletionCursor(t, source, "bundle.web", "bundle.")

	list, rpcErr := CompleteForText(path, source, pos, NewProjectCache(root))
	require.Nil(t, rpcErr)
	requireCompletionLabels(t, list, "web")
	requireNotCompletionLabels(t, list, "lookup", "deploy")
}

func TestCompletionUBCompositeBodyKeys(t *testing.T) {
	root, path, source, _ := definitionProject(t)
	source, pos := sourceWithCompletionCursor(t, source, "name: local.name", "n")

	list, rpcErr := CompleteForText(path, source, pos, NewProjectCache(root))
	require.Nil(t, rpcErr)
	requireCompletionLabels(t, list, "name")
	requireNotCompletionLabels(t, list, "@core", "action", "input", "local", "resource")
}

func TestCompletionUBCompositeValuesUseInputTypes(t *testing.T) {
	root, path, source, _ := definitionProject(t)
	source, pos := sourceWithCompletionCursor(t, source, "name: local.name", "name: ")

	list, rpcErr := CompleteForText(path, source, pos, NewProjectCache(root))
	require.Nil(t, rpcErr)
	requireCompletionLabels(t, list, "input.region", "local.name")
	requireNotCompletionLabels(t, list, "@core", "input", "local", "resource")
}

func TestCompletionGoBackedExpressionSelectorsUseFunctionsOnly(t *testing.T) {
	root, path, source, _ := goDefinitionProject(t)
	source, pos := sourceWithCompletionCursor(t, source, "def.slug('v1')", "def.")

	list, rpcErr := CompleteForText(path, source, pos, NewProjectCache(root))
	require.Nil(t, rpcErr)
	requireCompletionLabels(t, list, "slug")
	requireNotCompletionLabels(t, list, "server", "lookup", "deploy")
}

func TestCompletionSelectorWithMissingCachedSourceReturnsEmptyList(t *testing.T) {
	_, path, source, cache := missingCachedGoDefinitionProject(t)
	source, pos := sourceWithCompletionCursor(t, source, "def.slug('v1')", "def.")

	list, rpcErr := CompleteForText(path, source, pos, cache)
	require.Nil(t, rpcErr)
	require.Empty(t, list.Items)
}

func TestCompletionGoBackedBodyFields(t *testing.T) {
	root, path, source, _ := goDefinitionProject(t)

	list, rpcErr := CompleteForText(path, source,
		positionInText(source, "server-name: 'web'", "server-name"), NewProjectCache(root))
	require.Nil(t, rpcErr)
	requireCompletionLabels(t, list, "server-name")
	requireNotCompletionLabels(t, list, "id", "settings")
}

func TestCompletionUnknownGoNodeSelectorReturnsEmptyList(t *testing.T) {
	root, path, source, _ := goDefinitionProjectFixture(
		t, "testdata/ub/definition/valid/go-backed-unknown-node.ub",
	)

	list, rpcErr := CompleteForText(path, source,
		positionInText(source, "server-name: 'web'", "server-name"), NewProjectCache(root))
	require.Nil(t, rpcErr)
	require.Empty(t, list.Items)
}

func TestCompletionGoBackedConfigFields(t *testing.T) {
	root, path, source, _ := goDefinitionProject(t)

	list, rpcErr := CompleteForText(path, source,
		positionInText(source, "region: 'us-east-1'", "region"), NewProjectCache(root))
	require.Nil(t, rpcErr)
	requireCompletionLabels(t, list, "region")
	requireNotCompletionLabels(t, list, "retry")
}

func TestCompletionDoesNotFetchRemotes(t *testing.T) {
	root, path, source := completionProject(t)
	remote := &failingCachedRemote{t: t}
	cache := newProjectCacheWithRemote(root, func() (cachedRemoteSource, error) {
		return remote, nil
	})

	_, rpcErr := CompleteForText(path, source,
		positionInText(source, "value: null", "null"), cache)
	require.Nil(t, rpcErr)
}

func TestSessionCompletionInvalidSourceReturnsEmptyList(t *testing.T) {
	root := writeUBProject(t, nil, nil)
	path := filepath.Join(root, "factory.ub")
	uri := PathToFileURI(path)
	source := ubtest.ReadFixture(t,
		"testdata/ub/completion/invalid/incomplete-resource.ub")
	session := NewSession("dev")
	rpcErr := openDocument(t, session, uri, 1, source)
	require.Nil(t, rpcErr)

	result, rpcErr := requestCompletion(t, session, uri,
		positionInText(source, "file", "file"))
	require.Nil(t, rpcErr)
	list, ok := result.(protocol.CompletionList)
	require.True(t, ok)
	require.Empty(t, list.Items)
}

func TestSessionCompletionReturnsItems(t *testing.T) {
	_, path, source := completionProject(t)
	session := NewSession("dev")
	uri := PathToFileURI(path)
	rpcErr := openDocument(t, session, uri, 1, source)
	require.Nil(t, rpcErr)

	result, rpcErr := requestCompletion(t, session, uri,
		positionInText(source, "input.region", "region"))
	require.Nil(t, rpcErr)
	list, ok := result.(protocol.CompletionList)
	require.True(t, ok)
	requireCompletionLabels(t, list, "region", "count")
}

func completionProject(t *testing.T) (string, string, string) {
	t.Helper()
	root := writeUBProject(t, &deps.Project{
		Requires: map[deps.Dependency]deps.Requirement{},
		Replace:  map[deps.Dependency]string{},
	}, nil)
	source := ubtest.ReadFixture(t, "testdata/ub/completion/valid/factory.ub")
	path := filepath.Join(root, "factory.ub")
	require.NoError(t, os.WriteFile(path, []byte(source), 0o644))
	return root, path, source
}

func inputDeclarationCompletionProject(t *testing.T) (string, string, string) {
	t.Helper()
	root := writeUBProject(t, &deps.Project{
		Requires: map[deps.Dependency]deps.Requirement{},
		Replace:  map[deps.Dependency]string{},
	}, nil)
	source := ubtest.ReadValidFixture(t, "testdata/ub/completion", "input-declaration-fields")
	path := filepath.Join(root, "factory.ub")
	require.NoError(t, os.WriteFile(path, []byte(source), 0o644))
	return root, path, source
}

func inputDeclarationSourceWithPrefix(
	t *testing.T,
	source string,
	inserted string,
) (string, protocol.Position) {
	t.Helper()
	marker := "      type: library-config"
	offset := strings.Index(source, marker)
	require.NotEqual(t, -1, offset)
	line := "      " + inserted
	source = source[:offset] + line + "\n" + source[offset:]
	return source, OffsetToLSP(source, offset+len(line))
}

func inlineInputDeclarationSourceWithPrefix(
	t *testing.T,
	source string,
	inserted string,
) (string, protocol.Position) {
	t.Helper()
	marker := "type: string"
	offset := strings.Index(source, marker)
	require.NotEqual(t, -1, offset)
	offset += len(marker)
	prefix := " " + inserted
	source = source[:offset] + prefix + source[offset:]
	return source, OffsetToLSP(source, offset+len(prefix))
}

func sourceWithCompletionCursor(
	t *testing.T,
	source string,
	old string,
	new string,
) (string, protocol.Position) {
	t.Helper()
	offset := strings.Index(source, old)
	require.NotEqual(t, -1, offset)
	source = source[:offset] + new + source[offset+len(old):]
	return source, OffsetToLSP(source, offset+len(new))
}

func requestCompletion(
	t *testing.T,
	session *Session,
	uri string,
	pos protocol.Position,
) (any, *protocol.ResponseError) {
	t.Helper()
	params := protocol.CompletionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
		Position:     pos,
	}
	body, err := json.Marshal(params)
	require.NoError(t, err)
	return session.HandleRequest(context.Background(), &protocol.RequestMessage{
		JSONRPC: "2.0", Method: "textDocument/completion", Params: body,
	})
}

func requireCompletionLabels(
	t *testing.T,
	list protocol.CompletionList,
	labels ...string,
) {
	t.Helper()
	byLabel := map[string]bool{}
	for _, item := range list.Items {
		byLabel[item.Label] = true
	}
	for _, label := range labels {
		require.Truef(t, byLabel[label], "missing completion label %q in %#v", label, list.Items)
	}
}

func requireOnlyCompletionLabels(
	t *testing.T,
	list protocol.CompletionList,
	labels ...string,
) {
	t.Helper()
	got := make([]string, 0, len(list.Items))
	for _, item := range list.Items {
		got = append(got, item.Label)
	}
	require.ElementsMatch(t, labels, got)
}

func requireCompletionItem(
	t *testing.T,
	list protocol.CompletionList,
	label string,
) protocol.CompletionItem {
	t.Helper()
	for _, item := range list.Items {
		if item.Label == label {
			return item
		}
	}
	t.Fatalf("missing completion label %q in %#v", label, list.Items)
	return protocol.CompletionItem{}
}

func applyCompletionTextEdit(
	t *testing.T,
	source string,
	item protocol.CompletionItem,
	inserted string,
) string {
	t.Helper()
	require.NotNil(t, item.TextEdit)
	start, ok := LSPToOffset(source, item.TextEdit.Range.Start)
	require.True(t, ok)
	end, ok := LSPToOffset(source, item.TextEdit.Range.End)
	require.True(t, ok)
	require.Equal(t, inserted, source[start:end])
	edited := source[:start] + item.TextEdit.NewText + source[end:]
	require.NotContains(t, edited, "@@for-each")
	return edited[start : start+len(item.TextEdit.NewText)]
}

func requireNotCompletionLabels(
	t *testing.T,
	list protocol.CompletionList,
	labels ...string,
) {
	t.Helper()
	byLabel := map[string]bool{}
	for _, item := range list.Items {
		byLabel[item.Label] = true
	}
	for _, label := range labels {
		require.Falsef(t, byLabel[label], "unexpected completion label %q in %#v", label, list.Items)
	}
}

type failingCachedRemote struct {
	t *testing.T
}

func (r *failingCachedRemote) CachedSource(
	ref *resolve.RemoteImport,
	commit string,
) (*resolve.Source, bool, error) {
	r.t.Helper()
	r.t.Fatalf("completion fetched remote source %s at %s", ref.URL, commit)
	return nil, false, nil
}
