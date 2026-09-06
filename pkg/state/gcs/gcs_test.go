package gcs

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	internalconfig "github.com/cloudboss/unobin/internal/configuration"
	encodedvalue "github.com/cloudboss/unobin/pkg/encoding/value"
	"github.com/cloudboss/unobin/pkg/encrypters"
	sdkencrypt "github.com/cloudboss/unobin/pkg/sdk/encrypt"
	sdkstate "github.com/cloudboss/unobin/pkg/sdk/state"
)

const (
	testBucket  = "test-bucket"
	testPrefix  = "unobin"
	testFactory = "cluster-deploy"
	testStack   = "default"
	stackDir    = testPrefix + "/" + testFactory + "/" + testStack
)

func sampleSnapshotV2(t *testing.T) *sdkstate.SnapshotV2 {
	t.Helper()
	snapshot, err := sdkstate.NewSnapshotV2(
		sdkstate.FactoryInfo{
			Name:            testFactory,
			Version:         "v2.0.3",
			ContentRevision: "abc123def456",
		},
		"prod-east-alpha",
	)
	require.NoError(t, err)
	snapshot.GeneratedAt = time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	inputs, err := encodedvalue.Object(map[string]encodedvalue.Value{
		"name": encodedvalue.String("prod"),
	})
	require.NoError(t, err)
	outputs, err := encodedvalue.Object(map[string]encodedvalue.Value{
		"id": encodedvalue.String("network-abc"),
	})
	require.NoError(t, err)
	configurationValue, err := encodedvalue.Object(map[string]encodedvalue.Value{
		"region": encodedvalue.String("us-east-1"),
	})
	require.NoError(t, err)
	configuration, err := internalconfig.Build(sdkstate.ConfigurationRecord{
		Address:        "library-config.gcp",
		LibraryPath:    "example.com/gcp",
		SchemaVersion:  1,
		SchemaDigest:   strings.Repeat("a", 64),
		Value:          configurationValue,
		SensitivePaths: []string{},
	}, nil, nil)
	require.NoError(t, err)
	stableID := "network-abc"
	require.NoError(t, snapshot.SetEntry(sdkstate.StateEntryV2{
		Address: "resource.main",
		Kind:    sdkstate.StateResource,
		Payload: sdkstate.StatePayload{
			Kind: sdkstate.StateResource,
			Resource: &sdkstate.ResourceStatePayload{Target: sdkstate.ResourceTarget{
				Binding: sdkstate.CanonicalBinding{
					LibraryPath: "example.com/gcp", Export: "network",
				},
				SchemaVersion: 1,
				Inputs:        inputs,
				Outputs:       outputs,
				Configuration: configuration,
				Identity: sdkstate.IdentityRecord{
					DefinitionDigest: strings.Repeat("b", 64),
					Version:          1,
					StableID:         &stableID,
				},
				DependsOn:            []string{},
				SensitiveInputPaths:  []string{},
				SensitiveOutputPaths: []string{},
			}},
		},
	}))
	require.NoError(t, snapshot.Validate())
	return snapshot
}

func testStore(t *testing.T) (*Store, *fakeGCS) {
	t.Helper()
	return testStoreKMS(t, "")
}

func testStoreKMS(t *testing.T, kmsKeyName string) (*Store, *fakeGCS) {
	t.Helper()
	fake := newFakeGCS()
	store, err := NewStore(
		fake, testBucket, testPrefix, kmsKeyName, testFactory, testStack, encrypters.Noop{})
	require.NoError(t, err)
	return store, fake
}

func freezeClock(t *testing.T, at time.Time) {
	t.Helper()
	now = func() time.Time { return at }
	t.Cleanup(func() { now = time.Now })
}

func setKey(t *testing.T, envVar string) {
	t.Helper()
	key := make([]byte, 32)
	_, err := rand.Read(key)
	require.NoError(t, err)
	t.Setenv(envVar, base64.StdEncoding.EncodeToString(key))
}

func TestStoreRequiredArguments(t *testing.T) {
	fakeClient := newFakeGCS()
	enc := encrypters.Noop{}
	tests := []struct {
		name    string
		client  client
		bucket  string
		factory string
		stack   string
		enc     sdkencrypt.Encrypter
		want    string
	}{
		{name: "missing client", bucket: "b", factory: "f", stack: "s", enc: enc,
			want: "client is required"},
		{name: "missing bucket", client: fakeClient, factory: "f", stack: "s", enc: enc,
			want: "bucket is required"},
		{name: "missing factory", client: fakeClient, bucket: "b", stack: "s", enc: enc,
			want: "factory is required"},
		{name: "missing stack", client: fakeClient, bucket: "b", factory: "f", enc: enc,
			want: "stack is required"},
		{name: "missing encrypter", client: fakeClient, bucket: "b", factory: "f", stack: "s",
			want: "encrypter is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewStore(tt.client, tt.bucket, "", "", tt.factory, tt.stack, tt.enc)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestStorePathLayout(t *testing.T) {
	store, fake := testStore(t)
	rev, err := store.WriteV2(sampleSnapshotV2(t))
	require.NoError(t, err)
	assert.Equal(t, testBucket, store.Bucket)
	assert.Contains(t, fake.objectKeys(), stackDir+"/snapshots/"+rev+".json.enc")
}

func TestStoreEmptyPrefix(t *testing.T) {
	fake := newFakeGCS()
	store, err := NewStore(
		fake, testBucket, "", "", testFactory, testStack, encrypters.Noop{})
	require.NoError(t, err)
	rev, err := store.WriteV2(sampleSnapshotV2(t))
	require.NoError(t, err)
	assert.Contains(t, fake.objectKeys(), testFactory+"/"+testStack+"/snapshots/"+rev+".json.enc")
}

func TestStoreCurrentEmpty(t *testing.T) {
	store, _ := testStore(t)
	_, err := store.CurrentRev()
	require.ErrorIs(t, err, sdkstate.ErrNoCurrent)
}

func TestStoreWriteAndReadV2(t *testing.T) {
	store, _ := testStore(t)
	snapshot := sampleSnapshotV2(t)

	revision, err := store.WriteV2(snapshot)
	require.NoError(t, err)
	require.NotEmpty(t, revision)

	got, err := store.GetV2(revision)
	require.NoError(t, err)
	assert.Equal(t, snapshot, got)
}

func TestStoreGetV2RejectsVersionOneSnapshot(t *testing.T) {
	store, _ := testStore(t)
	body, err := os.ReadFile("testdata/snapshot-v1.json")
	require.NoError(t, err)
	sealed, err := sdkstate.Seal(body, sdkstate.PayloadTypeState, encrypters.Noop{})
	require.NoError(t, err)
	revision, err := store.writeSealedSnapshot(sealed)
	require.NoError(t, err)

	_, err = store.GetV2(revision)
	require.ErrorContains(t, err, "obsolete alpha format")
}

func TestStoreWriteV2RejectsNilSnapshot(t *testing.T) {
	store, _ := testStore(t)

	_, err := store.WriteV2(nil)
	require.ErrorContains(t, err, "snapshot is required")
}

func TestStoreSetCurrent(t *testing.T) {
	store, _ := testStore(t)
	snap := sampleSnapshotV2(t)
	rev, err := store.WriteV2(snap)
	require.NoError(t, err)
	require.NoError(t, store.SetCurrent(rev))

	gotRev, err := store.CurrentRev()
	require.NoError(t, err)
	assert.Equal(t, rev, gotRev)

	got, err := store.GetV2(gotRev)
	require.NoError(t, err)
	assert.Equal(t, snap, got)
}

func TestStoreSetCurrentRejectsUnknownRev(t *testing.T) {
	store, _ := testStore(t)
	err := store.SetCurrent("2026-01-01T00:00:00Z")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "set-current")
}

func TestStoreDelete(t *testing.T) {
	store, _ := testStore(t)
	rev, err := store.WriteV2(sampleSnapshotV2(t))
	require.NoError(t, err)
	require.NoError(t, store.Delete(rev))
	_, err = store.GetV2(rev)
	require.Error(t, err)
	require.NoError(t, store.Delete(rev))
}

func TestStoreDistinctRevsWhenClockStandsStill(t *testing.T) {
	store, _ := testStore(t)
	frozen := time.Date(2026, 5, 1, 10, 0, 0, 123456789, time.UTC)
	freezeClock(t, frozen)
	first, err := store.WriteV2(sampleSnapshotV2(t))
	require.NoError(t, err)
	second, err := store.WriteV2(sampleSnapshotV2(t))
	require.NoError(t, err)
	third, err := store.WriteV2(sampleSnapshotV2(t))
	require.NoError(t, err)
	assert.Equal(t, first+"_1", second)
	assert.Equal(t, first+"_2", third)
}

func TestStoreListChronological(t *testing.T) {
	store, _ := testStore(t)
	base := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	var want []string
	for i := range 3 {
		freezeClock(t, base.Add(time.Duration(i)*time.Second))
		rev, err := store.WriteV2(sampleSnapshotV2(t))
		require.NoError(t, err)
		want = append(want, rev)
	}
	got, err := store.List()
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestStoreCurrentSurvivesNewWrites(t *testing.T) {
	store, _ := testStore(t)
	first, err := store.WriteV2(sampleSnapshotV2(t))
	require.NoError(t, err)
	require.NoError(t, store.SetCurrent(first))
	_, err = store.WriteV2(sampleSnapshotV2(t))
	require.NoError(t, err)

	rev, err := store.CurrentRev()
	require.NoError(t, err)
	assert.Equal(t, first, rev)
}

func TestStoreLockBlocksUntilUnlock(t *testing.T) {
	store, _ := testStore(t)
	first, err := store.Lock(context.Background())
	require.NoError(t, err)

	got := make(chan sdkstate.Lock, 1)
	errs := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		lock, err := store.Lock(ctx)
		if err != nil {
			errs <- err
			return
		}
		got <- lock
	}()

	select {
	case lock := <-got:
		_ = lock.Unlock()
		t.Fatal("second lock acquired before first unlock")
	case err := <-errs:
		t.Fatalf("second lock failed before first unlock: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	require.NoError(t, first.Unlock())
	select {
	case lock := <-got:
		require.NoError(t, lock.Unlock())
	case err := <-errs:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("second lock did not acquire after unlock")
	}
}

func TestStoreLockContextErrorNamesHolder(t *testing.T) {
	store, _ := testStore(t)
	lock, err := store.Lock(context.Background())
	require.NoError(t, err)
	defer func() { _ = lock.Unlock() }()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, err = store.Lock(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "state locked by")
	assert.Contains(t, err.Error(), "state force-unlock")
}

func TestStoreForceUnlock(t *testing.T) {
	store, _ := testStore(t)
	_, err := store.Lock(context.Background())
	require.NoError(t, err)
	require.NoError(t, store.ForceUnlock())
	lock, err := store.Lock(context.Background())
	require.NoError(t, err)
	require.NoError(t, lock.Unlock())
}

func TestUnlockUsesRecordedGeneration(t *testing.T) {
	store, fake := testStore(t)
	lock, err := store.Lock(context.Background())
	require.NoError(t, err)
	fake.bumpGeneration(stackDir + "/lock")

	err = lock.Unlock()
	require.ErrorIs(t, err, errPrecondition)
}

func TestKMSKeyNameOnEveryPut(t *testing.T) {
	keyName := "projects/p/locations/us/keyRings/r/cryptoKeys/k"
	store, fake := testStoreKMS(t, keyName)
	rev, err := store.WriteV2(sampleSnapshotV2(t))
	require.NoError(t, err)
	require.NoError(t, store.SetCurrent(rev))
	lock, err := store.Lock(context.Background())
	require.NoError(t, err)
	require.NoError(t, lock.Unlock())

	puts := fake.recordedPuts()
	require.Len(t, puts, 3)
	for _, put := range puts {
		assert.Equal(t, keyName, put.kmsKeyName, "put of %s", put.key)
	}
}

func TestStoreWithEnvKeyEncrypter(t *testing.T) {
	setKey(t, "TEST_GCS_STATE_KEY")
	enc, err := encrypters.NewEnvKey("TEST_GCS_STATE_KEY")
	require.NoError(t, err)
	fake := newFakeGCS()
	store, err := NewStore(
		fake, testBucket, testPrefix, "", testFactory, testStack, enc)
	require.NoError(t, err)

	snap := sampleSnapshotV2(t)
	rev, err := store.WriteV2(snap)
	require.NoError(t, err)
	got, err := store.GetV2(rev)
	require.NoError(t, err)
	assert.Equal(t, snap, got)

	body, ok := fake.object(stackDir + "/snapshots/" + rev + ".json.enc")
	require.True(t, ok)
	assert.NotContains(t, string(body), "network-abc")

	var env sdkstate.Envelope
	require.NoError(t, json.Unmarshal(body, &env))
	assert.Equal(t, sdkstate.PayloadTypeState, env.PayloadType)
	require.NotNil(t, env.Encrypter, "snapshot should record the key source that sealed it")
	assert.Equal(t, "env-key", env.Encrypter.Name)
	assert.Equal(t, "TEST_GCS_STATE_KEY", env.Encrypter.Body["env-var"])
}
