package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// planAndApply runs Plan and ApplyPlan back to back and returns the
// apply result. The plan is round-tripped through EncodePlan and
// DecodePlan so the apply consumes the same byte form a real plan
// file would. Errors at any stage surface to the caller.
func planAndApply(exec *Executor) (*ExecResult, error) {
	ctx := context.Background()
	plan, err := exec.Plan(ctx)
	if err != nil {
		return nil, err
	}
	encoded, err := EncodePlan(plan)
	if err != nil {
		return nil, err
	}
	pf, err := DecodePlan(encoded)
	if err != nil {
		return nil, err
	}
	return exec.ApplyPlan(ctx, pf)
}

// applyOnce is planAndApply for tests that expect success; it requires
// no error and returns the apply result.
func applyOnce(t *testing.T, exec *Executor) *ExecResult {
	t.Helper()
	res, err := planAndApply(exec)
	require.NoError(t, err)
	return res
}
