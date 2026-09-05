package runtime

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPlanningReadDataSourceSharesConcurrentReads(t *testing.T) {
	pass := newPlanEvaluationV2Pass(newPlanEvaluationV2Facts())
	target := validPlannedDataSourceTarget(t)
	want := operationObject(t, map[string]EncodedValue{"id": StringValue("fresh")})
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	var reads atomic.Int32
	read := func(context.Context) (EncodedValue, error) {
		if reads.Add(1) == 1 {
			close(started)
		}
		<-release
		return want, nil
	}
	type result struct {
		outputs EncodedValue
		err     error
	}
	results := make(chan result, 8)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	go func() {
		outputs, err := pass.readDataSource(ctx, "data-source.image", target, read)
		results <- result{outputs, err}
	}()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	waiter, stopWaiting := context.WithCancel(ctx)
	stopWaiting()
	_, err := pass.readDataSource(waiter, "data-source.image", target, read)
	require.ErrorIs(t, err, context.Canceled)
	for range 7 {
		go func() {
			outputs, err := pass.readDataSource(ctx, "data-source.image", target, read)
			results <- result{outputs, err}
		}()
	}
	unblock()
	for range 8 {
		select {
		case result := <-results:
			require.NoError(t, result.err)
			require.Equal(t, want, result.outputs)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	require.Equal(t, int32(1), reads.Load())
}

func TestPlanningReadDataSourceCachesFailures(t *testing.T) {
	for _, name := range []string{"error", "panic", "invalid outputs"} {
		t.Run(name, func(t *testing.T) {
			pass := newPlanEvaluationV2Pass(newPlanEvaluationV2Facts())
			target := validPlannedDataSourceTarget(t)
			reads := 0
			read := func(context.Context) (EncodedValue, error) {
				reads++
				switch name {
				case "panic":
					panic("read panic")
				case "invalid outputs":
					return StringValue("invalid"), nil
				default:
					return EncodedValue{}, errors.New("read failed")
				}
			}
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			_, first := pass.readDataSource(ctx, "data-source.image", target, read)
			require.Error(t, first)
			_, second := pass.readDataSource(ctx, "data-source.image", target, read)
			require.ErrorIs(t, second, first)
			require.Equal(t, 1, reads)
		})
	}
}
