package gcs

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/api/option"
	"google.golang.org/api/storage/v1"
)

func newTestRESTClient(t *testing.T, handler http.HandlerFunc) client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	service, err := storage.NewService(
		context.Background(),
		option.WithEndpoint(server.URL+"/"),
		option.WithoutAuthentication(),
	)
	require.NoError(t, err)
	return newRESTClient(service, testBucket)
}

func TestRESTClientPutCreateOnlyUsesGenerationPrecondition(t *testing.T) {
	client := newTestRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "0", r.URL.Query().Get("ifGenerationMatch"))
		assert.Equal(t, "state/current", r.URL.Query().Get("name"))
		_, _ = io.ReadAll(r.Body)
		writeJSON(t, w, map[string]string{"name": "state/current", "generation": "7"})
	})

	info, err := client.putObject(
		context.Background(), "state/current", []byte("rev\n"), putOptions{createOnly: true})
	require.NoError(t, err)
	assert.Equal(t, objectInfo{name: "state/current", generation: 7}, info)
}

func TestRESTClientPutSendsKMSKeyName(t *testing.T) {
	keyName := "projects/p/locations/us/keyRings/r/cryptoKeys/k"
	client := newTestRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, keyName, r.URL.Query().Get("kmsKeyName"))
		_, _ = io.ReadAll(r.Body)
		writeJSON(t, w, map[string]string{"name": "state/current", "generation": "8"})
	})

	_, err := client.putObject(
		context.Background(), "state/current", []byte("rev\n"), putOptions{kmsKeyName: keyName})
	require.NoError(t, err)
}

func TestRESTClientGetDownloadsMedia(t *testing.T) {
	client := newTestRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "media", r.URL.Query().Get("alt"))
		_, err := w.Write([]byte("object bytes"))
		require.NoError(t, err)
	})

	got, err := client.getObject(context.Background(), "state/current")
	require.NoError(t, err)
	assert.Equal(t, []byte("object bytes"), got)
}

func TestRESTClientListPages(t *testing.T) {
	client := newTestRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "state/", r.URL.Query().Get("prefix"))
		switch r.URL.Query().Get("pageToken") {
		case "":
			writeJSON(t, w, map[string]any{
				"items": []map[string]string{
					{"name": "state/a", "generation": "1"},
				},
				"nextPageToken": "next",
			})
		case "next":
			writeJSON(t, w, map[string]any{
				"items": []map[string]string{
					{"name": "state/b", "generation": "2"},
				},
			})
		default:
			t.Fatalf("unexpected page token %q", r.URL.Query().Get("pageToken"))
		}
	})

	got, err := client.listObjects(context.Background(), "state/")
	require.NoError(t, err)
	assert.Equal(t, []objectInfo{
		{name: "state/a", generation: 1},
		{name: "state/b", generation: 2},
	}, got)
}

func TestRESTClientDeleteUsesGenerationPrecondition(t *testing.T) {
	client := newTestRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Equal(t, "7", r.URL.Query().Get("ifGenerationMatch"))
		w.WriteHeader(http.StatusNoContent)
	})

	err := client.deleteObject(context.Background(), "state/current", deleteOptions{generation: 7})
	require.NoError(t, err)
}

func TestRESTClientMapsNotFoundAndPrecondition(t *testing.T) {
	notFound := newTestRESTClient(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "missing", http.StatusNotFound)
	})
	_, err := notFound.getObject(context.Background(), "missing")
	require.ErrorIs(t, err, errNotFound)

	precondition := newTestRESTClient(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "conflict", http.StatusPreconditionFailed)
	})
	_, err = precondition.putObject(
		context.Background(), "state/current", []byte("rev\n"), putOptions{createOnly: true})
	require.ErrorIs(t, err, errPrecondition)
}

func writeJSON(t *testing.T, w http.ResponseWriter, body any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	require.NoError(t, json.NewEncoder(w).Encode(body))
}
