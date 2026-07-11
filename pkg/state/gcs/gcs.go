// Package gcs stores state snapshots in Google Cloud Storage.
package gcs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path"
	"strings"
	"time"

	sdkencrypt "github.com/cloudboss/unobin/pkg/sdk/encrypt"
	sdkstate "github.com/cloudboss/unobin/pkg/sdk/state"
)

const (
	maxRevAttempts = 100
	lockPollEvery  = 500 * time.Millisecond
	snapshotSuffix = ".json.enc"
)

var now = time.Now

var (
	errNotFound     = errors.New("gcs: object not found")
	errPrecondition = errors.New("gcs: precondition failed")
)

var _ sdkstate.Backend = (*Store)(nil)

type objectInfo struct {
	name       string
	generation int64
}

type putOptions struct {
	createOnly bool
	kmsKeyName string
}

type deleteOptions struct {
	generation int64
}

type client interface {
	getObject(ctx context.Context, key string) ([]byte, error)
	putObject(ctx context.Context, key string, body []byte, opts putOptions) (objectInfo, error)
	deleteObject(ctx context.Context, key string, opts deleteOptions) error
	listObjects(ctx context.Context, prefix string) ([]objectInfo, error)
}

// Store reads and writes snapshots under a per-stack key prefix.
type Store struct {
	Bucket     string
	Prefix     string
	KMSKeyName string

	client client
	stack  string
	enc    sdkencrypt.Encrypter
	dir    string
}

// NewStore returns a Store for factory and stack in bucket.
func NewStore(
	client client,
	bucket, prefix, kmsKeyName, factory, stack string,
	enc sdkencrypt.Encrypter,
) (*Store, error) {
	if client == nil {
		return nil, errors.New("gcs store: client is required")
	}
	if bucket == "" {
		return nil, errors.New("gcs store: bucket is required")
	}
	if factory == "" {
		return nil, errors.New("gcs store: factory is required")
	}
	if stack == "" {
		return nil, errors.New("gcs store: stack is required")
	}
	if enc == nil {
		return nil, errors.New("gcs store: encrypter is required")
	}
	return &Store{
		Bucket:     bucket,
		Prefix:     prefix,
		KMSKeyName: kmsKeyName,
		client:     client,
		stack:      stack,
		enc:        enc,
		dir:        path.Join(prefix, factory, stack),
	}, nil
}

func (s *Store) Stack() string { return s.stack }

func (s *Store) Current() (*sdkstate.Snapshot, error) {
	rev, err := s.currentRev()
	if err != nil {
		return nil, err
	}
	return s.Get(rev)
}

func (s *Store) CurrentRev() (string, error) {
	return s.currentRev()
}

func (s *Store) Get(rev string) (*sdkstate.Snapshot, error) {
	sealed, err := s.client.getObject(context.Background(), s.snapshotKey(rev))
	if err != nil {
		return nil, fmt.Errorf("gcs store: get %s: %w", rev, err)
	}
	body, err := sdkstate.Open(
		sealed,
		sdkstate.PayloadTypeState,
		func(*sdkstate.Ref) (sdkencrypt.Encrypter, error) {
			return s.enc, nil
		},
	)
	if err != nil {
		return nil, fmt.Errorf("gcs store: open %s: %w", rev, err)
	}
	return sdkstate.DecodeSnapshot(body)
}

func (s *Store) Write(snap *sdkstate.Snapshot) (string, error) {
	body, err := sdkstate.EncodeSnapshot(snap)
	if err != nil {
		return "", err
	}
	sealed, err := sdkstate.Seal(body, sdkstate.PayloadTypeState, s.enc)
	if err != nil {
		return "", err
	}
	base := now().UTC().Format(time.RFC3339Nano)
	rev := base
	for attempt := range maxRevAttempts {
		if attempt > 0 {
			rev = fmt.Sprintf("%s_%d", base, attempt)
		}
		_, err := s.client.putObject(context.Background(), s.snapshotKey(rev), sealed, putOptions{
			createOnly: true,
			kmsKeyName: s.KMSKeyName,
		})
		if err == nil {
			return rev, nil
		}
		if errors.Is(err, errPrecondition) {
			continue
		}
		return "", fmt.Errorf("gcs store: write %s: %w", rev, err)
	}
	return "", fmt.Errorf("gcs store: could not allocate fresh revision after %d attempts",
		maxRevAttempts)
}

func (s *Store) SetCurrent(rev string) error {
	if _, err := s.client.getObject(context.Background(), s.snapshotKey(rev)); err != nil {
		return fmt.Errorf("set-current %s: %w", rev, err)
	}
	_, err := s.client.putObject(
		context.Background(), s.key("current"), []byte(rev+"\n"), s.putOptions(false))
	if err != nil {
		return fmt.Errorf("set-current %s: %w", rev, err)
	}
	return nil
}

func (s *Store) List() ([]string, error) {
	prefix := s.key("snapshots") + "/"
	infos, err := s.client.listObjects(context.Background(), prefix)
	if err != nil {
		return nil, fmt.Errorf("gcs store: list: %w", err)
	}
	out := make([]string, 0, len(infos))
	for _, info := range infos {
		name := strings.TrimPrefix(info.name, prefix)
		if rev, ok := strings.CutSuffix(name, snapshotSuffix); ok {
			out = append(out, rev)
		}
	}
	return sdkstate.SortRevisions(out), nil
}

func (s *Store) Delete(rev string) error {
	err := s.client.deleteObject(context.Background(), s.snapshotKey(rev), deleteOptions{})
	if err != nil && !errors.Is(err, errNotFound) {
		return fmt.Errorf("gcs store: delete %s: %w", rev, err)
	}
	return nil
}

type lockInfo struct {
	ID      string    `json:"id"`
	Who     string    `json:"who"`
	Created time.Time `json:"created"`
}

func (s *Store) Lock(ctx context.Context) (sdkstate.Lock, error) {
	info := lockInfo{ID: randomID(), Who: whoAmI(), Created: now().UTC()}
	body, err := json.Marshal(info)
	if err != nil {
		return nil, err
	}
	key := s.key("lock")
	for {
		obj, err := s.client.putObject(ctx, key, body, s.putOptions(true))
		if err == nil {
			return &gcsLock{store: s, key: key, generation: obj.generation}, nil
		}
		if !errors.Is(err, errPrecondition) {
			return nil, fmt.Errorf("gcs store: lock: %w", err)
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

func (s *Store) ForceUnlock() error {
	err := s.client.deleteObject(context.Background(), s.key("lock"), deleteOptions{})
	if err != nil && !errors.Is(err, errNotFound) {
		return err
	}
	return nil
}

type gcsLock struct {
	store      *Store
	key        string
	generation int64
}

func (l *gcsLock) Unlock() error {
	return l.store.client.deleteObject(
		context.Background(), l.key, deleteOptions{generation: l.generation})
}

func (s *Store) key(parts ...string) string {
	return path.Join(append([]string{s.dir}, parts...)...)
}

func (s *Store) snapshotKey(rev string) string {
	return s.key("snapshots", rev+snapshotSuffix)
}

func (s *Store) putOptions(createOnly bool) putOptions {
	return putOptions{createOnly: createOnly, kmsKeyName: s.KMSKeyName}
}

func (s *Store) currentRev() (string, error) {
	body, err := s.client.getObject(context.Background(), s.key("current"))
	if err != nil {
		if errors.Is(err, errNotFound) {
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

func (s *Store) holderDescription() string {
	body, err := s.client.getObject(context.Background(), s.key("lock"))
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
