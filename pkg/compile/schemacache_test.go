package compile

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	ubruntime "github.com/cloudboss/unobin/pkg/runtime"
)

func TestSchemaCacheReadsEachPathOnce(t *testing.T) {
	var calls []string
	c := &SchemaCache{
		read: func(sourcePath string) (*ubruntime.LibrarySchema, []string, error) {
			calls = append(calls, sourcePath)
			return &ubruntime.LibrarySchema{}, []string{"warning for " + sourcePath}, nil
		},
		entries: map[string]schemaCacheEntry{},
	}

	first, warnings, err := c.Read("lib/disk")
	require.NoError(t, err)
	require.NotNil(t, first)
	require.Equal(t, []string{"warning for lib/disk"}, warnings)

	again, warnings, err := c.Read("lib/disk")
	require.NoError(t, err)
	require.Same(t, first, again)
	require.Equal(t, []string{"warning for lib/disk"}, warnings)

	_, _, err = c.Read("lib/net")
	require.NoError(t, err)
	require.Equal(t, []string{"lib/disk", "lib/net"}, calls)
}

func TestSchemaCacheDoesNotStoreFailures(t *testing.T) {
	readFailed := errors.New("read failed")
	calls := 0
	c := &SchemaCache{
		read: func(string) (*ubruntime.LibrarySchema, []string, error) {
			calls++
			return nil, nil, readFailed
		},
		entries: map[string]schemaCacheEntry{},
	}

	_, _, err := c.Read("lib/disk")
	require.ErrorIs(t, err, readFailed)
	_, _, err = c.Read("lib/disk")
	require.ErrorIs(t, err, readFailed)
	require.Equal(t, 2, calls)
}
