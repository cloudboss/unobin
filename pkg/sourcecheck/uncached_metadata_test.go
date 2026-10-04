package sourcecheck

import (
	"errors"
	"fmt"
	"io/fs"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/diagnostic"
	"github.com/cloudboss/unobin/pkg/libraryapi"
	"github.com/cloudboss/unobin/pkg/resolve"
)

func TestDeferUncachedMetadata(t *testing.T) {
	uncached := &uncachedModuleError{}
	unsupported := &libraryapi.UnsupportedMajorError{Required: libraryapi.Version{Major: 2}}
	rejected := diagnostic.WithDiagnostics(unsupported, diagnostic.Diagnostic{
		Code: "unobin.library-api.unsupported-major", Severity: diagnostic.SeverityError,
		Message: "cached library requires API 2.0",
	})
	for _, test := range []struct {
		name     string
		err      error
		deferred bool
		retained error
	}{
		{name: "none"},
		{name: "cached failure", err: rejected, retained: rejected},
		{name: "uncached", err: uncached, deferred: true},
		{name: "wrapped uncached", err: fmt.Errorf("configuration: %w", uncached), deferred: true},
		{name: "cached and uncached", err: errors.Join(uncached, rejected),
			deferred: true, retained: rejected},
		{name: "wrapped mixed failures", err: fmt.Errorf("library: %w",
			errors.Join(rejected, uncached)), deferred: true, retained: rejected},
		{name: "operational and uncached", err: errors.Join(uncached, fs.ErrPermission),
			deferred: true, retained: fs.ErrPermission},
	} {
		t.Run(test.name, func(t *testing.T) {
			deferred, err := deferUncachedMetadata(test.err)
			assert.Equal(t, test.deferred, deferred)
			if test.retained == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, test.retained)
				assert.Equal(t, diagnostic.FromError(test.retained, diagnostic.ConvertOptions{}),
					diagnostic.FromError(err, diagnostic.ConvertOptions{}))
			}
			if !test.deferred {
				assert.Equal(t, test.err, err)
			}
		})
	}
}

func TestCachedModuleResolver(t *testing.T) {
	resolver := newTestResolver(t, t.TempDir())
	ref := &resolve.RemoteImport{URL: "example.com/configs", Version: "v1.0.0"}
	cached := cachedModuleResolver{wrapped: resolver}
	_, err := cached.Resolve(ref)
	require.ErrorContains(t, err, "test resolver: no source")
	var uncached *uncachedModuleError
	assert.False(t, errors.As(err, &uncached))
	for _, source := range []*resolve.Source{nil, opaqueSource()} {
		resolver.remotes[ref.URL] = source
		_, err = cached.Resolve(ref)
		require.ErrorAs(t, err, &uncached)
		assert.Equal(t, "selected module source is not cached", err.Error())
	}
	source := &resolve.Source{Path: t.TempDir()}
	resolver.remotes[ref.URL] = source
	got, err := cached.Resolve(ref)
	require.NoError(t, err)
	assert.Same(t, source, got)
}
