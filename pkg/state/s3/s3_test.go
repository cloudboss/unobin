package s3

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/encrypters"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
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

func sampleSnapshot() *sdkstate.Snapshot {
	return &sdkstate.Snapshot{
		FormatVersion: sdkstate.CurrentFormatVersion,
		Factory: sdkstate.FactoryInfo{
			Name:            "cluster-deploy",
			Version:         "v2.0.3",
			ContentRevision: "abc123def456",
		},
		Stack:       "prod-east-alpha",
		GeneratedAt: time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC),
		Entries: []*sdkstate.Entry{
			{
				Address:       "resource.main",
				Type:          sdkstate.EntryLeaf,
				Kind:          "resource",
				Selector:      &sdkstate.Selector{Alias: "aws", Export: "vpc"},
				SchemaVersion: 1,
				Inputs:        map[string]any{"cidr-block": "10.0.0.0/16"},
				Outputs:       map[string]any{"id": "vpc-abc"},
			},
		},
	}
}

func setKey(t *testing.T, envVar string) {
	t.Helper()
	key := make([]byte, 32)
	_, err := rand.Read(key)
	require.NoError(t, err)
	t.Setenv(envVar, base64.StdEncoding.EncodeToString(key))
}

// testClient builds a real S3 client against the fake server, through
// awscfg the same way the backend builds it.
func testClient(t *testing.T, url string) *s3.Client {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(dir, "config"))
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(dir, "credentials"))
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("AWS_ACCESS_KEY_ID", "test-key")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test-secret")
	awsCfg, err := awscfg.Load(context.Background(), &awscfg.Configuration{
		Region:      &cfg.String{Value: "us-east-1"},
		EndpointURL: &cfg.String{Value: url},
	})
	require.NoError(t, err)
	return s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.UsePathStyle = true
	})
}

func testStoreKMS(t *testing.T, kmsKeyID string) (*Store, *fakeS3) {
	t.Helper()
	fake := newFakeS3()
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	client := testClient(t, srv.URL)
	store, err := NewStore(
		client, testBucket, testPrefix, kmsKeyID, testFactory, testStack, encrypters.Noop{})
	require.NoError(t, err)
	return store, fake
}

func testStore(t *testing.T) (*Store, *fakeS3) {
	t.Helper()
	return testStoreKMS(t, "")
}

func freezeClock(t *testing.T, at time.Time) {
	t.Helper()
	now = func() time.Time { return at }
	t.Cleanup(func() { now = time.Now })
}

func TestStoreRequiredArguments(t *testing.T) {
	client := &s3.Client{}
	enc := encrypters.Noop{}
	tests := []struct {
		name    string
		client  *s3.Client
		bucket  string
		factory string
		stack   string
		enc     sdkencrypt.Encrypter
		want    string
	}{
		{name: "missing client", bucket: "b", factory: "f", stack: "s", enc: enc,
			want: "client is required"},
		{name: "missing bucket", client: client, factory: "f", stack: "s", enc: enc,
			want: "bucket is required"},
		{name: "missing factory", client: client, bucket: "b", stack: "s", enc: enc,
			want: "factory is required"},
		{name: "missing stack", client: client, bucket: "b", factory: "f", enc: enc,
			want: "stack is required"},
		{name: "missing encrypter", client: client, bucket: "b", factory: "f", stack: "s",
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
	rev, err := store.Write(sampleSnapshot())
	require.NoError(t, err)
	require.NoError(t, store.SetCurrent(rev))
	keys := fake.objectKeys(testBucket)
	assert.Contains(t, keys, stackDir+"/current")
	assert.Contains(t, keys, stackDir+"/snapshots/"+rev+".json.enc")
}

func TestStoreEmptyPrefix(t *testing.T) {
	fake := newFakeS3()
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	store, err := NewStore(
		testClient(t, srv.URL), testBucket, "", "", testFactory, testStack, encrypters.Noop{})
	require.NoError(t, err)
	rev, err := store.Write(sampleSnapshot())
	require.NoError(t, err)
	assert.Contains(t, fake.objectKeys(testBucket),
		testFactory+"/"+testStack+"/snapshots/"+rev+".json.enc")
}

func TestStoreCurrentEmpty(t *testing.T) {
	store, _ := testStore(t)
	_, err := store.Current()
	require.ErrorIs(t, err, sdkstate.ErrNoCurrent)
	_, err = store.CurrentRev()
	require.ErrorIs(t, err, sdkstate.ErrNoCurrent)
}

func TestStoreWriteAndRead(t *testing.T) {
	store, _ := testStore(t)
	snap := sampleSnapshot()
	rev, err := store.Write(snap)
	require.NoError(t, err)
	require.NotEmpty(t, rev)

	got, err := store.Get(rev)
	require.NoError(t, err)
	assert.Equal(t, snap, got)
}

func TestStoreSetCurrent(t *testing.T) {
	store, _ := testStore(t)
	snap := sampleSnapshot()
	rev, err := store.Write(snap)
	require.NoError(t, err)
	require.NoError(t, store.SetCurrent(rev))

	gotRev, err := store.CurrentRev()
	require.NoError(t, err)
	assert.Equal(t, rev, gotRev)

	got, err := store.Current()
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
	rev, err := store.Write(sampleSnapshot())
	require.NoError(t, err)
	require.NoError(t, store.Delete(rev))
	_, err = store.Get(rev)
	require.Error(t, err)
	require.NoError(t, store.Delete(rev))
}

func TestStoreDistinctRevsWhenClockStandsStill(t *testing.T) {
	store, _ := testStore(t)
	freezeClock(t, time.Date(2026, 5, 1, 10, 0, 0, 123456789, time.UTC))
	first, err := store.Write(sampleSnapshot())
	require.NoError(t, err)
	second, err := store.Write(sampleSnapshot())
	require.NoError(t, err)
	third, err := store.Write(sampleSnapshot())
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
		rev, err := store.Write(sampleSnapshot())
		require.NoError(t, err)
		want = append(want, rev)
	}
	got, err := store.List()
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestStoreCurrentSurvivesNewWrites(t *testing.T) {
	store, _ := testStore(t)
	first, err := store.Write(sampleSnapshot())
	require.NoError(t, err)
	require.NoError(t, store.SetCurrent(first))
	_, err = store.Write(sampleSnapshot())
	require.NoError(t, err)

	rev, err := store.CurrentRev()
	require.NoError(t, err)
	assert.Equal(t, first, rev)
}

func TestStoreWithEnvKeyEncrypter(t *testing.T) {
	setKey(t, "TEST_S3_STATE_KEY")
	enc, err := encrypters.NewEnvKey("TEST_S3_STATE_KEY")
	require.NoError(t, err)
	fake := newFakeS3()
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	store, err := NewStore(
		testClient(t, srv.URL), testBucket, testPrefix, "", testFactory, testStack, enc)
	require.NoError(t, err)

	snap := sampleSnapshot()
	rev, err := store.Write(snap)
	require.NoError(t, err)
	got, err := store.Get(rev)
	require.NoError(t, err)
	assert.Equal(t, snap, got)

	body, ok := fake.object(testBucket, stackDir+"/snapshots/"+rev+".json.enc")
	require.True(t, ok)
	assert.NotContains(t, string(body), "vpc-abc")

	var env sdkstate.Envelope
	require.NoError(t, json.Unmarshal(body, &env))
	require.NotNil(t, env.Encrypter, "snapshot should record the key source that sealed it")
	assert.Equal(t, "env-key", env.Encrypter.Name)
	assert.Equal(t, "TEST_S3_STATE_KEY", env.Encrypter.Body["env-var"])
}

func TestStoreLockExcludesSecondHolder(t *testing.T) {
	store, _ := testStore(t)
	lock, err := store.Lock(context.Background())
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err = store.Lock(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "state locked by")
	assert.Contains(t, err.Error(), "state force-unlock")

	require.NoError(t, lock.Unlock())
	relock, err := store.Lock(context.Background())
	require.NoError(t, err)
	require.NoError(t, relock.Unlock())
}

func TestStoreLockBlocksUntilReleased(t *testing.T) {
	store, _ := testStore(t)
	lock, err := store.Lock(context.Background())
	require.NoError(t, err)

	released := make(chan struct{})
	go func() {
		time.Sleep(100 * time.Millisecond)
		_ = lock.Unlock()
		close(released)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	second, err := store.Lock(ctx)
	require.NoError(t, err)
	<-released
	require.NoError(t, second.Unlock())
}

func TestStoreLockHolderInfo(t *testing.T) {
	store, fake := testStore(t)
	lock, err := store.Lock(context.Background())
	require.NoError(t, err)
	defer func() { _ = lock.Unlock() }()

	body, ok := fake.object(testBucket, stackDir+"/lock")
	require.True(t, ok)
	var info struct {
		ID      string    `json:"id"`
		Who     string    `json:"who"`
		Created time.Time `json:"created"`
	}
	require.NoError(t, json.Unmarshal(body, &info))
	assert.NotEmpty(t, info.ID)
	assert.Contains(t, info.Who, "@")
	assert.False(t, info.Created.IsZero())
}

func TestStoreForceUnlockClearsLock(t *testing.T) {
	store, _ := testStore(t)
	_, err := store.Lock(context.Background())
	require.NoError(t, err)
	require.NoError(t, store.ForceUnlock())
	lock, err := store.Lock(context.Background())
	require.NoError(t, err)
	require.NoError(t, lock.Unlock())
}

func TestStoreForceUnlockNoLockIsOK(t *testing.T) {
	store, _ := testStore(t)
	require.NoError(t, store.ForceUnlock())
}

func TestStoreKMSHeadersOnEveryPut(t *testing.T) {
	keyID := "arn:aws:kms:us-east-1:123456789012:key/abc"
	store, fake := testStoreKMS(t, keyID)
	rev, err := store.Write(sampleSnapshot())
	require.NoError(t, err)
	require.NoError(t, store.SetCurrent(rev))
	lock, err := store.Lock(context.Background())
	require.NoError(t, err)
	require.NoError(t, lock.Unlock())

	puts := fake.recordedPuts()
	require.Len(t, puts, 3)
	for _, p := range puts {
		assert.Equal(t, "aws:kms", p.sse, "put of %s", p.key)
		assert.Equal(t, keyID, p.kmsKeyID, "put of %s", p.key)
	}
}

func TestStoreNoSSEHeadersWithoutKey(t *testing.T) {
	store, fake := testStore(t)
	_, err := store.Write(sampleSnapshot())
	require.NoError(t, err)
	for _, p := range fake.recordedPuts() {
		assert.Empty(t, p.sse, "put of %s", p.key)
		assert.Empty(t, p.kmsKeyID, "put of %s", p.key)
	}
}

func TestStoreSnapshotsCreatedConditionally(t *testing.T) {
	store, fake := testStore(t)
	_, err := store.Write(sampleSnapshot())
	require.NoError(t, err)
	puts := fake.recordedPuts()
	require.Len(t, puts, 1)
	assert.Equal(t, "*", puts[0].ifNoneMatch)
}
