package runtime

import (
	"context"
	"errors"

	"github.com/cloudboss/unobin/pkg/diagnostic"
	"github.com/cloudboss/unobin/pkg/sdk/state"
)

type StateUnlockError struct {
	Cause error
}

func (e *StateUnlockError) Error() string {
	return "release lock: " + e.Cause.Error()
}

func AcquireStateLock(
	ctx context.Context,
	store state.Backend,
) (func(error) error, error) {
	if store == nil {
		return nil, errors.New("state store is required")
	}
	lock, err := store.Lock(ctx)
	if err != nil {
		return nil, diagnostic.Context("acquire lock", err)
	}
	if lock == nil {
		return nil, errors.New("acquire lock: backend returned a nil lock")
	}
	return func(operationErr error) error {
		unlockErr := lock.Unlock()
		if unlockErr == nil {
			return operationErr
		}
		return errors.Join(operationErr, &StateUnlockError{Cause: unlockErr})
	}, nil
}

func checkedCurrentRevision(store state.Backend) (string, error) {
	revision, err := store.CurrentRev()
	if errors.Is(err, state.ErrNoCurrent) {
		return "", nil
	}
	if err != nil {
		return "", diagnostic.Context("current revision", err)
	}
	return revision, nil
}
