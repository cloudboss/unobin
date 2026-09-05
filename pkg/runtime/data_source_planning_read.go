package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
)

type dataSourceReadResult struct {
	outputs EncodedValue
	err     error
	ready   chan struct{}
}

func (s *planningPassState) readDataSource(
	ctx context.Context,
	address string,
	target PlannedDataSourceTarget,
	read func(context.Context) (EncodedValue, error),
) (EncodedValue, error) {
	request := struct {
		Address       string              `json:"address"`
		Binding       Binding             `json:"binding"`
		Inputs        EncodedValue        `json:"inputs"`
		Configuration ConfigurationRecord `json:"configuration"`
	}{address, target.Binding, target.Inputs, *target.Configuration.Record}
	encoded, err := json.Marshal(request)
	if err != nil {
		return EncodedValue{}, fmt.Errorf("encode data-source read request: %w", err)
	}
	digest := sha256.Sum256(encoded)
	s.reads.mu.Lock()
	result, ok := s.reads.dataSources[digest]
	if ok {
		s.reads.mu.Unlock()
		select {
		case <-result.ready:
			return result.outputs, result.err
		case <-ctx.Done():
			return EncodedValue{}, ctx.Err()
		}
	}
	if s.reads.dataSources == nil {
		s.reads.dataSources = map[[32]byte]*dataSourceReadResult{}
	}
	result = &dataSourceReadResult{ready: make(chan struct{})}
	s.reads.dataSources[digest] = result
	s.reads.mu.Unlock()

	result.outputs, result.err = guard(
		"reading this data source during planning", false,
		func() (EncodedValue, error) { return read(ctx) },
	)
	if result.err == nil {
		result.err = validatePlanObject(result.outputs, "data-source outputs", false)
	}
	close(result.ready)
	return result.outputs, result.err
}
