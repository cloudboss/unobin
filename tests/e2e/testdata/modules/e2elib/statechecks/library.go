package statechecks

import (
	"context"
	"errors"

	"github.com/cloudboss/unobin/pkg/awscfg"
	"github.com/cloudboss/unobin/pkg/backends"
	"github.com/cloudboss/unobin/pkg/encrypters"
	ubruntime "github.com/cloudboss/unobin/pkg/runtime"
)

func Library() *ubruntime.Library {
	return &ubruntime.Library{
		Name:          "statechecks",
		Compatibility: ubruntime.LibraryCompatibility{RequiredAPI: "1.0"},
		Actions: map[string]ubruntime.ActionRegistration{
			"cancel-lock":  ubruntime.MakeAction[CancelLock, *LockResult, ubruntime.NoConfig](),
			"reject-stack": ubruntime.MakeAction[RejectStack, *StackResult, ubruntime.NoConfig](),
		},
	}
}

type CancelLock struct{}

type LockResult struct {
	Canceled bool
}

func (*CancelLock) Run(ctx context.Context, _ ubruntime.NoConfig) (*LockResult, error) {
	region, pathStyle := "us-east-1", true
	backend := backends.Backends()[backends.S3Name]
	store, err := backend.New(&backends.S3BackendConfig{
		Bucket: "test-bucket", UsePathStyle: &pathStyle,
		AWS: &awscfg.Configuration{Region: &region},
	}, "lock-cancellation", "test", encrypters.Noop{})
	if err != nil {
		return nil, err
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	lock, err := store.Lock(canceled)
	if lock != nil {
		if unlockErr := lock.Unlock(); unlockErr != nil {
			return nil, unlockErr
		}
	}
	if err == nil {
		return nil, errors.New("canceled state lock was acquired")
	}
	if !errors.Is(err, context.Canceled) {
		return nil, err
	}
	return &LockResult{Canceled: true}, nil
}
