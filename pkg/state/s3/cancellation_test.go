package s3

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/encrypters"
)

func TestStoreLockRejectsCanceledContext(t *testing.T) {
	store, fake := testStore(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	lock, err := store.Lock(ctx)
	assert.ErrorIs(t, err, context.Canceled)
	_, exists := fake.object(testBucket, stackDir+"/lock")
	assert.False(t, exists)
	if lock != nil {
		require.NoError(t, lock.Unlock())
	}
}

func blockedStore(t *testing.T) (*Store, <-chan struct{}, func()) {
	t.Helper()
	started := make(chan struct{})
	released := make(chan struct{})
	start := sync.OnceFunc(func() { close(started) })
	release := sync.OnceFunc(func() { close(released) })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		start()
		select {
		case <-r.Context().Done():
		case <-released:
		}
		w.Header().Set("ETag", `"test"`)
	}))
	t.Cleanup(server.Close)
	t.Cleanup(release)
	store, err := NewStore(testClient(t, server.URL), testBucket, testPrefix, "",
		testFactory, testStack, encrypters.Noop{})
	require.NoError(t, err)
	return store, started, release
}

func TestStoreLockCancelsRequest(t *testing.T) {
	store, started, release := blockedStore(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		lock, err := store.Lock(ctx)
		if lock != nil {
			err = errors.Join(err, lock.Unlock())
		}
		result <- err
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("lock request did not start")
	}
	cancel()
	select {
	case err := <-result:
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		release()
		err := <-result
		assert.ErrorIs(t, err, context.Canceled)
		t.Error("lock request did not stop after cancellation")
	}
}

func TestStoreLockHolderLookupHasDeadline(t *testing.T) {
	store, started, release := blockedStore(t)
	result := make(chan string, 1)
	go func() { result <- store.holderDescription() }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("holder request did not start")
	}
	select {
	case holder := <-result:
		assert.Empty(t, holder)
	case <-time.After(3 * time.Second):
		release()
		<-result
		t.Error("holder lookup did not stop at its deadline")
	}
}
