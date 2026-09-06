package runtime

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/cloudboss/unobin/pkg/sdk/state"
)

// RefreshV2 updates recorded resource observations under the stack state lock.
func (e *Executor) RefreshV2(ctx context.Context) (result *RefreshResult, err error) {
	if ctx == nil {
		return nil, fmt.Errorf("refresh context is required")
	}
	if e == nil || e.LibraryCatalog == nil {
		return nil, fmt.Errorf("factory library catalog is required")
	}
	if e.Store == nil {
		return nil, fmt.Errorf("state store is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	snapshots, err := applyPlanFileV2Snapshots(e.Store)
	if err != nil {
		return nil, err
	}
	release, err := AcquireStateLock(ctx, e.Store)
	if err != nil {
		return nil, err
	}
	res := &RefreshResult{}
	writtenRevision := ""
	defer func() {
		if result == nil && writtenRevision != "" {
			current, currentErr := checkedCurrentRevision(e.Store)
			if currentErr != nil {
				err = errors.Join(err, currentErr)
			} else if current == writtenRevision {
				res.WrittenRev = current
				result = res
			}
		}
		err = release(err)
	}()
	revision, err := checkedCurrentRevision(e.Store)
	if err != nil {
		return nil, err
	}
	if revision == "" {
		return res, nil
	}
	snapshot, err := snapshots.Load(revision)
	if err != nil {
		return nil, fmt.Errorf("load version 2 snapshot: %w", err)
	}
	if snapshot == nil {
		return nil, fmt.Errorf("version 2 snapshot loader returned nil")
	}
	snapshot, err = snapshot.Clone()
	if err != nil {
		return nil, err
	}
	snapshot.Factory, snapshot.Stack = e.Factory, e.Store.Stack()
	if err := snapshot.Validate(); err != nil {
		return nil, fmt.Errorf("refresh snapshot: %w", err)
	}
	if err := e.validateFactoryV2Bindings(snapshot, true); err != nil {
		return nil, err
	}
	requests, err := e.prepareRefreshResourcesV2(snapshot)
	if err != nil {
		return nil, err
	}
	results := make([]refreshResourceV2Result, len(requests))
	sem := make(chan struct{}, e.effectiveParallelism())
	var wait sync.WaitGroup
	for i, request := range requests {
		sem <- struct{}{}
		wait.Go(func() {
			defer func() { <-sem }()
			results[i].target, results[i].err = guard(
				"refreshing a version 2 resource", true,
				func() (*ResourceTarget, error) { return request.read(ctx) },
			)
		})
	}
	wait.Wait()
	for i, refreshed := range results {
		if refreshed.err != nil {
			return nil, fmt.Errorf("%s: %w", requests[i].address, refreshed.err)
		}
		address := requests[i].address
		if refreshed.target == nil {
			if err := snapshot.RemoveEntry(address); err != nil {
				return nil, err
			}
			res.Dropped++
			continue
		}
		entry := snapshot.Find(address)
		entry.Payload.Resource.Target = *refreshed.target
		res.Refreshed++
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	snapshot.GeneratedAt = time.Now().UTC()
	if err := snapshot.Validate(); err != nil {
		return nil, err
	}
	writtenRevision, err = snapshots.Write(snapshot)
	if err != nil {
		return nil, fmt.Errorf("write refreshed snapshot: %w", err)
	}
	if err := snapshots.SetCurrent(writtenRevision); err != nil {
		return nil, fmt.Errorf("set current refreshed snapshot: %w", err)
	}
	res.WrittenRev = writtenRevision
	return res, nil
}

type refreshResourceV2Request struct {
	address       string
	target        ResourceTarget
	registration  *resourceDefinitionRegistration
	configuration any
}

type refreshResourceV2Result struct {
	target *ResourceTarget
	err    error
}

func (e *Executor) prepareRefreshResourcesV2(
	snapshot *state.SnapshotV2,
) ([]refreshResourceV2Request, error) {
	requests := make([]refreshResourceV2Request, 0, len(snapshot.Entries))
	for _, entry := range snapshot.Entries {
		if entry.Kind != state.StateResource {
			continue
		}
		prior := entry.Payload.Resource.Target
		registration, definition, err := e.LibraryCatalog.resource(prior.Binding)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", entry.Address, err)
		}
		target, configuration, err := prepareRegisteredResourcePriorTarget(
			prior, definition, registration,
		)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", entry.Address, err)
		}
		requests = append(requests, refreshResourceV2Request{
			address: entry.Address, target: target,
			registration: registration, configuration: configuration,
		})
	}
	return requests, nil
}

func (r refreshResourceV2Request) read(ctx context.Context) (*ResourceTarget, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	observation, err := r.registration.readResourceObservation(ctx, resourceReadRequest{
		Address: r.address, Binding: r.target.Binding, Inputs: r.target.Inputs,
		Configuration: r.target.Configuration, PriorOutputs: r.target.Outputs,
	}, r.configuration)
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := validatePlanningIdentity(
		r.target.Identity, observation.Identity, "refreshed",
	); err != nil {
		return nil, err
	}
	target := r.target
	target.Outputs, target.Identity = *observation.Outputs, *observation.Identity
	if err := target.Validate(); err != nil {
		return nil, err
	}
	return &target, nil
}
