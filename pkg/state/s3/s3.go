// Package s3 stores state snapshots in an S3 bucket. The layout
// under the configured prefix mirrors the local store's directory
// layout, one object per snapshot plus a current pointer and a lock
// marker:
//
//	[<prefix>/]<factory>/<stack>/
//	  current             // Object holding the rev of the current snapshot.
//	  lock                // Lock marker with holder info, present while held.
//	  snapshots/
//	    <rev>.json.enc    // rev is an RFC3339Nano timestamp.
//
// Exclusion relies on S3 conditional writes: the lock marker and each
// snapshot are created with If-None-Match, so a concurrent create
// loses with a precondition failure instead of clobbering. Stores
// without conditional-write support cannot hold the lock safely and
// fail at Lock with the store's own error.
package s3

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"

	sdkencrypt "github.com/cloudboss/unobin/pkg/sdk/encrypt"
	sdkstate "github.com/cloudboss/unobin/pkg/sdk/state"
)

const (
	maxRevAttempts = 100
	lockPollEvery  = 500 * time.Millisecond
	snapshotSuffix = ".json.enc"
)

// now returns the current time. Tests override it to freeze the clock
// and force the rev allocator to disambiguate collisions structurally.
var now = time.Now

var _ sdkstate.Backend = (*Store)(nil)

// Store reads and writes snapshots under a per-stack key prefix in
// one bucket. KMSKeyID, when set, requests SSE-KMS with that key on
// every object written, the lock marker and current pointer included,
// so bucket policies that deny unencrypted puts hold.
type Store struct {
	Bucket   string
	Prefix   string
	KMSKeyID string

	client *s3.Client
	stack  string
	enc    sdkencrypt.Encrypter
	dir    string
}

// NewStore returns an Store for the given factory and stack in
// bucket, with all objects under prefix when it is not empty. The
// encrypter is required, but a pass-through (encrypters.Noop) can be
// passed for tests.
func NewStore(
	client *s3.Client,
	bucket, prefix, kmsKeyID, factory, stack string,
	enc sdkencrypt.Encrypter,
) (*Store, error) {
	if client == nil {
		return nil, errors.New("s3 store: client is required")
	}
	if bucket == "" {
		return nil, errors.New("s3 store: bucket is required")
	}
	if factory == "" {
		return nil, errors.New("s3 store: factory is required")
	}
	if stack == "" {
		return nil, errors.New("s3 store: stack is required")
	}
	if enc == nil {
		return nil, errors.New("s3 store: encrypter is required")
	}
	return &Store{
		Bucket:   bucket,
		Prefix:   prefix,
		KMSKeyID: kmsKeyID,
		client:   client,
		stack:    stack,
		enc:      enc,
		dir:      path.Join(prefix, factory, stack),
	}, nil
}

// Stack returns the stack name this store was constructed
// for. Required by the Backend interface.
func (s *Store) Stack() string { return s.stack }

// Current returns the snapshot named by the current pointer. Returns
// sdkstate.ErrNoCurrent when no snapshot has been written yet.
func (s *Store) Current() (*sdkstate.Snapshot, error) {
	rev, err := s.currentRev()
	if err != nil {
		return nil, err
	}
	return s.Get(rev)
}

// CurrentRev returns the rev the current pointer names, or
// sdkstate.ErrNoCurrent.
func (s *Store) CurrentRev() (string, error) {
	return s.currentRev()
}

// Get returns the snapshot with the given rev.
func (s *Store) Get(rev string) (*sdkstate.Snapshot, error) {
	sealed, err := s.getObject(s.snapshotKey(rev))
	if err != nil {
		return nil, fmt.Errorf("s3 store: get %s: %w", rev, err)
	}
	body, err := sdkstate.Open(sealed, func(*sdkstate.Ref) (sdkencrypt.Encrypter, error) {
		return s.enc, nil
	})
	if err != nil {
		return nil, fmt.Errorf("s3 store: open %s: %w", rev, err)
	}
	return sdkstate.DecodeSnapshot(body)
}

// Write commits snap to the bucket and returns its rev. The caller
// advances the current pointer with SetCurrent. Each rev starts as an
// RFC3339Nano timestamp; the snapshot object is created with
// If-None-Match, and on a precondition failure (two writes sharing
// the same nanosecond) a numeric suffix is appended until the create
// wins, so uniqueness does not depend on the clock advancing between
// writes.
func (s *Store) Write(snap *sdkstate.Snapshot) (string, error) {
	body, err := sdkstate.EncodeSnapshot(snap)
	if err != nil {
		return "", err
	}
	sealed, err := sdkstate.Seal(body, nil, s.enc)
	if err != nil {
		return "", err
	}
	base := now().UTC().Format(time.RFC3339Nano)
	rev := base
	for attempt := range maxRevAttempts {
		if attempt > 0 {
			rev = fmt.Sprintf("%s_%d", base, attempt)
		}
		err := s.putObject(s.snapshotKey(rev), sealed, true)
		if err == nil {
			return rev, nil
		}
		if errCodeIs(err, "PreconditionFailed", "ConditionalRequestConflict") {
			continue
		}
		return "", fmt.Errorf("s3 store: write %s: %w", rev, err)
	}
	return "", fmt.Errorf("s3 store: could not allocate fresh revision after %d attempts",
		maxRevAttempts)
}

// SetCurrent atomically points "current" at the named rev. The
// snapshot must already exist.
func (s *Store) SetCurrent(rev string) error {
	key := s.snapshotKey(rev)
	_, err := s.client.HeadObject(context.Background(), &s3.HeadObjectInput{
		Bucket: aws.String(s.Bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("set-current %s: %w", rev, err)
	}
	if err := s.putObject(s.key("current"), []byte(rev+"\n"), false); err != nil {
		return fmt.Errorf("set-current %s: %w", rev, err)
	}
	return nil
}

// List returns the revs of every stored snapshot in chronological
// order. S3 lists keys lexically, which is chronological for
// RFC3339Nano revs.
func (s *Store) List() ([]string, error) {
	prefix := s.key("snapshots") + "/"
	var out []string
	p := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.Bucket),
		Prefix: aws.String(prefix),
	})
	for p.HasMorePages() {
		page, err := p.NextPage(context.Background())
		if err != nil {
			return nil, fmt.Errorf("s3 store: list: %w", err)
		}
		for _, obj := range page.Contents {
			name := strings.TrimPrefix(aws.ToString(obj.Key), prefix)
			if rev, ok := strings.CutSuffix(name, snapshotSuffix); ok {
				out = append(out, rev)
			}
		}
	}
	return out, nil
}

// Delete removes the snapshot with the given rev. Removing a rev that
// does not exist is not an error.
func (s *Store) Delete(rev string) error {
	_, err := s.client.DeleteObject(context.Background(), &s3.DeleteObjectInput{
		Bucket: aws.String(s.Bucket),
		Key:    aws.String(s.snapshotKey(rev)),
	})
	if err != nil {
		return fmt.Errorf("s3 store: delete %s: %w", rev, err)
	}
	return nil
}

// lockInfo is the JSON body of the lock marker, so an operator who
// hits contention can see who holds the lock and since when.
type lockInfo struct {
	ID      string    `json:"id"`
	Who     string    `json:"who"`
	Created time.Time `json:"created"`
}

// Lock acquires the stack's exclusive lock by creating the lock
// marker with If-None-Match. Lock blocks until the create wins or ctx
// is canceled; while blocked it polls on the same cadence as the
// local store. A canceled wait names the holder in its error.
func (s *Store) Lock(ctx context.Context) (sdkstate.Lock, error) {
	info := lockInfo{ID: randomID(), Who: whoAmI(), Created: now().UTC()}
	body, err := json.Marshal(info)
	if err != nil {
		return nil, err
	}
	key := s.key("lock")
	for {
		err := s.putObject(key, body, true)
		if err == nil {
			return &s3Lock{store: s, key: key}, nil
		}
		if !errCodeIs(err, "PreconditionFailed", "ConditionalRequestConflict") {
			return nil, fmt.Errorf("s3 store: lock: %w", err)
		}
		select {
		case <-ctx.Done():
			if holder := s.holderDescription(); holder != "" {
				return nil, fmt.Errorf("%w; %s", ctx.Err(), holder)
			}
			return nil, ctx.Err()
		case <-time.After(lockPollEvery):
		}
	}
}

// holderDescription reads the lock marker for an error message. Best
// effort: contention errors stay useful even when the marker vanished
// or does not parse.
func (s *Store) holderDescription() string {
	body, err := s.getObject(s.key("lock"))
	if err != nil {
		return ""
	}
	var info lockInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return "state locked; run 'state force-unlock' if the holder is gone"
	}
	return fmt.Sprintf(
		"state locked by %s since %s; run 'state force-unlock' if the holder is gone",
		info.Who, info.Created.Format(time.RFC3339))
}

// ForceUnlock removes the lock marker without checking who holds it.
// Operators run this to recover after a leaked lock and must ensure
// no concurrent run is in progress.
func (s *Store) ForceUnlock() error {
	_, err := s.client.DeleteObject(context.Background(), &s3.DeleteObjectInput{
		Bucket: aws.String(s.Bucket),
		Key:    aws.String(s.key("lock")),
	})
	return err
}

type s3Lock struct {
	store *Store
	key   string
}

func (l *s3Lock) Unlock() error {
	_, err := l.store.client.DeleteObject(context.Background(), &s3.DeleteObjectInput{
		Bucket: aws.String(l.store.Bucket),
		Key:    aws.String(l.key),
	})
	return err
}

func (s *Store) key(parts ...string) string {
	return path.Join(append([]string{s.dir}, parts...)...)
}

func (s *Store) snapshotKey(rev string) string {
	return s.key("snapshots", rev+snapshotSuffix)
}

func (s *Store) getObject(key string) ([]byte, error) {
	out, err := s.client.GetObject(context.Background(), &s3.GetObjectInput{
		Bucket: aws.String(s.Bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, err
	}
	defer out.Body.Close()
	return io.ReadAll(out.Body)
}

// putObject writes one object, with If-None-Match when create is
// true so an existing object wins, and with SSE-KMS headers when the
// store has a key configured.
func (s *Store) putObject(key string, body []byte, create bool) error {
	in := &s3.PutObjectInput{
		Bucket: aws.String(s.Bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(body),
	}
	if create {
		in.IfNoneMatch = aws.String("*")
	}
	if s.KMSKeyID != "" {
		in.ServerSideEncryption = s3types.ServerSideEncryptionAwsKms
		in.SSEKMSKeyId = aws.String(s.KMSKeyID)
	}
	_, err := s.client.PutObject(context.Background(), in)
	return err
}

func (s *Store) currentRev() (string, error) {
	body, err := s.getObject(s.key("current"))
	if err != nil {
		if isNotFound(err) {
			return "", sdkstate.ErrNoCurrent
		}
		return "", err
	}
	rev := strings.TrimSpace(string(body))
	if rev == "" {
		return "", sdkstate.ErrNoCurrent
	}
	return rev, nil
}

// errCodeIs reports whether err is an S3 API error with one of the
// given codes. The precondition codes are unmodeled in the SDK, so
// matching is by code string.
func errCodeIs(err error, codes ...string) bool {
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	code := apiErr.ErrorCode()
	return slices.Contains(codes, code)
}

// isNotFound matches both shapes S3 uses for a missing object:
// NoSuchKey from GetObject and bare NotFound from HeadObject.
func isNotFound(err error) bool {
	return errCodeIs(err, "NoSuchKey", "NotFound")
}

func randomID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("pid-%d", os.Getpid())
	}
	return hex.EncodeToString(b)
}

func whoAmI() string {
	username := "unknown"
	if u, err := user.Current(); err == nil && u.Username != "" {
		username = u.Username
	}
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "unknown"
	}
	return username + "@" + host
}
