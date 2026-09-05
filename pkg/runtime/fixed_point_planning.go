package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
)

const maxPlanningPasses = 32

type resourceReadRequest struct {
	Address       string              `json:"address"`
	Binding       Binding             `json:"binding"`
	Inputs        EncodedValue        `json:"inputs"`
	Configuration ConfigurationRecord `json:"configuration"`
	PriorOutputs  EncodedValue        `json:"prior-outputs"`
}

type resourceReadFunc func(
	context.Context,
	resourceReadRequest,
) (ResourceObservation, error)

type planningFacts struct {
	replacements map[string]map[string]bool
	invalidated  map[string]bool
}

type resourceReadResult struct {
	observation ResourceObservation
	err         error
	ready       chan struct{}
}

type planningReadCache struct {
	mu          sync.Mutex
	results     map[string]*resourceReadResult
	dataSources map[[32]byte]*dataSourceReadResult
}

type planningPassState struct {
	mu      sync.Mutex
	facts   *planningFacts
	reads   *planningReadCache
	changes map[string]bool
}

func runFixedPointPlanning[T any](
	ctx context.Context,
	evaluate func(*planningPassState) (T, error),
) (T, error) {
	var zero T
	if ctx == nil {
		return zero, fmt.Errorf("planning context is required")
	}
	if evaluate == nil {
		return zero, fmt.Errorf("planning evaluator is required")
	}
	facts := &planningFacts{
		replacements: map[string]map[string]bool{},
		invalidated:  map[string]bool{},
	}
	reads := &planningReadCache{results: map[string]*resourceReadResult{}}
	for pass := 1; pass <= maxPlanningPasses; pass++ {
		if err := ctx.Err(); err != nil {
			return zero, err
		}
		state := &planningPassState{
			facts:   facts,
			reads:   reads,
			changes: map[string]bool{},
		}
		result, err := evaluate(state)
		if err != nil {
			return zero, err
		}
		changes := state.passChanges()
		if len(changes) == 0 {
			return result, nil
		}
		if pass == maxPlanningPasses {
			return zero, fmt.Errorf(
				"planning did not converge after %d passes; final pass added %s",
				maxPlanningPasses,
				strings.Join(changes, ", "),
			)
		}
	}
	panic("unreachable")
}

func (s *planningPassState) replacementReasons(address string) []string {
	if s == nil || s.facts == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	reasons := make([]string, 0, len(s.facts.replacements[address]))
	for reason := range s.facts.replacements[address] {
		reasons = append(reasons, reason)
	}
	slices.Sort(reasons)
	return reasons
}

func (s *planningPassState) requireReplacement(address string, reasons ...string) error {
	if err := validateNodeAddress(address, NodeResource); err != nil {
		return err
	}
	if s == nil || s.facts == nil {
		return fmt.Errorf("planning pass state is required")
	}
	for _, reason := range reasons {
		if err := validateReplacementReasons([]string{reason}); err != nil {
			return err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.facts.replacements[address] == nil {
		s.facts.replacements[address] = map[string]bool{}
	}
	for _, reason := range reasons {
		if s.facts.replacements[address][reason] {
			continue
		}
		s.facts.replacements[address][reason] = true
		s.changes[address+" "+reason] = true
	}
	return nil
}

func (s *planningPassState) outputsInvalidated(address string) bool {
	if s == nil || s.facts == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.facts.invalidated[address]
}

func (s *planningPassState) invalidateOutputs(address string) error {
	if err := validateNodeAddress(address, NodeResource); err != nil {
		return err
	}
	if s == nil || s.facts == nil {
		return fmt.Errorf("planning pass state is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.facts.invalidated[address] {
		return nil
	}
	s.facts.invalidated[address] = true
	s.changes[address+" outputs"] = true
	return nil
}

func (s *planningPassState) readResource(
	ctx context.Context,
	request resourceReadRequest,
	read resourceReadFunc,
) (ResourceObservation, error) {
	if s == nil || s.reads == nil || s.reads.results == nil {
		return ResourceObservation{}, fmt.Errorf("planning pass state is required")
	}
	if ctx == nil {
		return ResourceObservation{}, fmt.Errorf("resource read context is required")
	}
	if read == nil {
		return ResourceObservation{}, fmt.Errorf("resource read callback is required")
	}
	digest, err := request.digest()
	if err != nil {
		return ResourceObservation{}, err
	}

	s.reads.mu.Lock()
	result, ok := s.reads.results[digest]
	if ok {
		s.reads.mu.Unlock()
		select {
		case <-result.ready:
			return cloneResourceObservation(result.observation), result.err
		case <-ctx.Done():
			return ResourceObservation{}, ctx.Err()
		}
	}
	result = &resourceReadResult{ready: make(chan struct{})}
	s.reads.results[digest] = result
	s.reads.mu.Unlock()

	observation, err := guard(
		"reading this resource during planning",
		false,
		func() (ResourceObservation, error) {
			return read(ctx, request)
		},
	)
	if errors.Is(err, ErrNotFound) {
		observation = ResourceObservation{Status: ObservationAbsent}
		err = nil
	}
	if err == nil {
		err = observation.Validate()
		if err != nil {
			err = fmt.Errorf("resource observation: %w", err)
		}
	}
	result.observation = cloneResourceObservation(observation)
	result.err = err
	close(result.ready)
	return cloneResourceObservation(observation), err
}

func (s *planningPassState) recordResourceOperation(
	address string,
	operation *ResourcePlanOperation,
) (*ResourcePlanOperation, error) {
	if operation == nil {
		return nil, nil
	}
	if err := validateNodeAddress(address, NodeResource); err != nil {
		return nil, err
	}
	result := *operation
	result.Reasons = slices.Clone(operation.Reasons)
	if result.Decision == DecisionReplace {
		if err := s.requireReplacement(address, result.Reasons...); err != nil {
			return nil, err
		}
	}

	reasons := s.replacementReasons(address)
	if len(reasons) > 0 &&
		result.Desired != nil &&
		result.Prior != nil &&
		result.Observation != nil &&
		result.Observation.Status == ObservationPresent {
		result.Decision = DecisionReplace
		result.Reasons = reasons
	}
	if err := result.Validate(); err != nil {
		return nil, err
	}
	switch result.Decision {
	case DecisionCreate, DecisionUpdate, DecisionReplace:
		if err := s.invalidateOutputs(address); err != nil {
			return nil, err
		}
	}
	return &result, nil
}

func (r resourceReadRequest) digest() (string, error) {
	if err := r.validate(); err != nil {
		return "", err
	}
	encoded, err := json.Marshal(r)
	if err != nil {
		return "", fmt.Errorf("encode resource read request: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func (r resourceReadRequest) validate() error {
	if err := validateNodeAddress(r.Address, NodeResource); err != nil {
		return err
	}
	if err := r.Binding.Validate(); err != nil {
		return fmt.Errorf("read binding: %w", err)
	}
	if err := validatePlanObject(r.Inputs, "read inputs", false); err != nil {
		return err
	}
	if err := r.Configuration.Validate(); err != nil {
		return fmt.Errorf("read configuration: %w", err)
	}
	if r.Configuration.LibraryPath != r.Binding.LibraryPath {
		return fmt.Errorf("read configuration library path does not match binding")
	}
	if err := validatePlanObject(r.PriorOutputs, "read prior outputs", false); err != nil {
		return err
	}
	return nil
}

func cloneResourceObservation(observation ResourceObservation) ResourceObservation {
	result := observation
	if observation.Outputs != nil {
		outputs := *observation.Outputs
		result.Outputs = &outputs
	}
	if observation.Identity != nil {
		identity := *observation.Identity
		identity.StableID = copyString(observation.Identity.StableID)
		result.Identity = &identity
	}
	return result
}

func (s *planningPassState) passChanges() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	changes := make([]string, 0, len(s.changes))
	for change := range s.changes {
		changes = append(changes, change)
	}
	slices.Sort(changes)
	return changes
}
