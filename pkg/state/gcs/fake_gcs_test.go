package gcs

import (
	"context"
	"sort"
	"strings"
	"sync"
)

type fakeGCS struct {
	mu         sync.Mutex
	objects    map[string]fakeGCSObject
	generation int64
	puts       []fakeGCSPut
}

type fakeGCSObject struct {
	body       []byte
	generation int64
}

type fakeGCSPut struct {
	key        string
	createOnly bool
	kmsKeyName string
	body       []byte
}

func newFakeGCS() *fakeGCS {
	return &fakeGCS{objects: map[string]fakeGCSObject{}}
}

func (f *fakeGCS) getObject(_ context.Context, key string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	obj, ok := f.objects[key]
	if !ok {
		return nil, errNotFound
	}
	return append([]byte(nil), obj.body...), nil
}

func (f *fakeGCS) putObject(
	_ context.Context,
	key string,
	body []byte,
	opts putOptions,
) (objectInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if opts.createOnly {
		if _, ok := f.objects[key]; ok {
			return objectInfo{}, errPrecondition
		}
	}
	f.generation++
	obj := fakeGCSObject{body: append([]byte(nil), body...), generation: f.generation}
	f.objects[key] = obj
	f.puts = append(f.puts, fakeGCSPut{
		key:        key,
		createOnly: opts.createOnly,
		kmsKeyName: opts.kmsKeyName,
		body:       append([]byte(nil), body...),
	})
	return objectInfo{name: key, generation: obj.generation}, nil
}

func (f *fakeGCS) deleteObject(_ context.Context, key string, opts deleteOptions) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	obj, ok := f.objects[key]
	if !ok {
		return errNotFound
	}
	if opts.generation != 0 && obj.generation != opts.generation {
		return errPrecondition
	}
	delete(f.objects, key)
	return nil
}

func (f *fakeGCS) listObjects(_ context.Context, prefix string) ([]objectInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	infos := make([]objectInfo, 0)
	for key, obj := range f.objects {
		if strings.HasPrefix(key, prefix) {
			infos = append(infos, objectInfo{name: key, generation: obj.generation})
		}
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].name < infos[j].name })
	return infos, nil
}

func (f *fakeGCS) object(key string) ([]byte, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	obj, ok := f.objects[key]
	if !ok {
		return nil, false
	}
	return append([]byte(nil), obj.body...), true
}

func (f *fakeGCS) objectKeys() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	keys := make([]string, 0, len(f.objects))
	for key := range f.objects {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func (f *fakeGCS) recordedPuts() []fakeGCSPut {
	f.mu.Lock()
	defer f.mu.Unlock()
	puts := make([]fakeGCSPut, len(f.puts))
	copy(puts, f.puts)
	return puts
}

func (f *fakeGCS) bumpGeneration(key string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	obj := f.objects[key]
	f.generation++
	obj.generation = f.generation
	f.objects[key] = obj
}
