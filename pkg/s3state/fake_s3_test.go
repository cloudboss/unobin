package s3state

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
)

// fakeS3 is an in-process S3 speaking just enough of the REST API for
// the store: path-style object reads and writes, ListObjectsV2, and
// If-None-Match conditional creates. The conditional check and the
// write happen under one lock, the same atomicity S3 provides, so
// lock-contention tests are deterministic.
type fakeS3 struct {
	mu      sync.Mutex
	objects map[string]fakeObject
	puts    []recordedPut
}

type fakeObject struct {
	body []byte
	etag string
}

// recordedPut keeps the headers of one PutObject so tests can assert
// the encryption headers went out on every write.
type recordedPut struct {
	key         string
	sse         string
	kmsKeyID    string
	ifNoneMatch string
}

func newFakeS3() *fakeS3 {
	return &fakeS3{objects: map[string]fakeObject{}}
}

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	bucket, key, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
	switch {
	case r.Method == http.MethodGet && key == "":
		f.list(w, r, bucket)
	case r.Method == http.MethodGet:
		f.get(w, bucket, key)
	case r.Method == http.MethodHead:
		f.head(w, bucket, key)
	case r.Method == http.MethodPut:
		f.put(w, r, bucket, key)
	case r.Method == http.MethodDelete:
		f.del(w, bucket, key)
	default:
		w.WriteHeader(http.StatusNotImplemented)
	}
}

func (f *fakeS3) put(w http.ResponseWriter, r *http.Request, bucket, key string) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.puts = append(f.puts, recordedPut{
		key:         key,
		sse:         r.Header.Get("x-amz-server-side-encryption"),
		kmsKeyID:    r.Header.Get("x-amz-server-side-encryption-aws-kms-key-id"),
		ifNoneMatch: r.Header.Get("If-None-Match"),
	})
	full := bucket + "/" + key
	if r.Header.Get("If-None-Match") == "*" {
		if _, exists := f.objects[full]; exists {
			writeS3Error(w, http.StatusPreconditionFailed,
				"PreconditionFailed", "object already exists")
			return
		}
	}
	sum := md5.Sum(body)
	etag := hex.EncodeToString(sum[:])
	f.objects[full] = fakeObject{body: body, etag: etag}
	w.Header().Set("ETag", `"`+etag+`"`)
	w.WriteHeader(http.StatusOK)
}

func (f *fakeS3) get(w http.ResponseWriter, bucket, key string) {
	f.mu.Lock()
	obj, ok := f.objects[bucket+"/"+key]
	f.mu.Unlock()
	if !ok {
		writeS3Error(w, http.StatusNotFound, "NoSuchKey", "no such key")
		return
	}
	w.Header().Set("ETag", `"`+obj.etag+`"`)
	_, _ = w.Write(obj.body)
}

func (f *fakeS3) head(w http.ResponseWriter, bucket, key string) {
	f.mu.Lock()
	obj, ok := f.objects[bucket+"/"+key]
	f.mu.Unlock()
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.Header().Set("ETag", `"`+obj.etag+`"`)
	w.Header().Set("Content-Length", fmt.Sprint(len(obj.body)))
	w.WriteHeader(http.StatusOK)
}

func (f *fakeS3) del(w http.ResponseWriter, bucket, key string) {
	f.mu.Lock()
	delete(f.objects, bucket+"/"+key)
	f.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

type listResult struct {
	XMLName     xml.Name  `xml:"ListBucketResult"`
	IsTruncated bool      `xml:"IsTruncated"`
	KeyCount    int       `xml:"KeyCount"`
	Contents    []listObj `xml:"Contents"`
}

type listObj struct {
	Key  string `xml:"Key"`
	Size int    `xml:"Size"`
}

func (f *fakeS3) list(w http.ResponseWriter, r *http.Request, bucket string) {
	prefix := r.URL.Query().Get("prefix")
	f.mu.Lock()
	var keys []string
	for full := range f.objects {
		b, key, _ := strings.Cut(full, "/")
		if b == bucket && strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
	}
	f.mu.Unlock()
	slices.Sort(keys)
	out := listResult{KeyCount: len(keys)}
	for _, k := range keys {
		out.Contents = append(out.Contents, listObj{Key: k, Size: 1})
	}
	w.Header().Set("Content-Type", "application/xml")
	body, _ := xml.Marshal(out)
	_, _ = w.Write(body)
}

func writeS3Error(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	fmt.Fprintf(w,
		`<?xml version="1.0" encoding="UTF-8"?><Error><Code>%s</Code><Message>%s</Message></Error>`,
		code, msg)
}

// object returns one stored body for assertions.
func (f *fakeS3) object(bucket, key string) ([]byte, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	obj, ok := f.objects[bucket+"/"+key]
	return obj.body, ok
}

// recordedPuts returns a copy of every PutObject seen so far.
func (f *fakeS3) recordedPuts() []recordedPut {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.puts)
}

// objectKeys returns the sorted keys stored in bucket.
func (f *fakeS3) objectKeys(bucket string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var keys []string
	for full := range f.objects {
		if b, key, _ := strings.Cut(full, "/"); b == bucket {
			keys = append(keys, key)
		}
	}
	slices.Sort(keys)
	return keys
}
