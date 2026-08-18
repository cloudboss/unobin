package runtime

import (
	"fmt"
	"math"
	"slices"
	"strings"
)

type preparedResourceDesired[In any] struct {
	Target PlannedResourceTarget
	Inputs In
}

type preparedResourcePrior[In, Out any] struct {
	Target  ResourceTarget
	Inputs  In
	Outputs Out
}

type preparedResourceObservation[Out any] struct {
	Observation ResourceObservation
	Outputs     Out
}

type resourcePlanningRequest struct {
	Desired                         *PlannedResourceTarget
	Prior                           *ResourceTarget
	RecordedObservation             *ResourceObservation
	DesiredConfigurationObservation *ResourceObservation
}

type preparedResourcePlanningRequest[In, Out any] struct {
	Desired                         *preparedResourceDesired[In]
	Prior                           *preparedResourcePrior[In, Out]
	RecordedObservation             *preparedResourceObservation[Out]
	DesiredConfigurationObservation *preparedResourceObservation[Out]
}

func (d resolvedResourceDefinition[In, Out, Config]) planResourceOperation(
	request resourcePlanningRequest,
) (*ResourcePlanOperation, error) {
	prepared, err := d.prepareResourcePlanningRequest(request)
	if err != nil {
		return nil, err
	}
	return d.planPreparedResourceOperation(prepared)
}

func (d resolvedResourceDefinition[In, Out, Config]) planPreparedResourceOperation(
	request preparedResourcePlanningRequest[In, Out],
) (*ResourcePlanOperation, error) {
	if request.Desired == nil && request.Prior == nil {
		if request.RecordedObservation != nil ||
			request.DesiredConfigurationObservation != nil {
			return nil, fmt.Errorf("resource without desired or prior target forbids observations")
		}
		return nil, nil
	}
	if request.Desired != nil {
		if err := request.Desired.Target.Validate(); err != nil {
			return nil, fmt.Errorf("desired target: %w", err)
		}
	}
	if request.Prior == nil {
		if request.RecordedObservation != nil ||
			request.DesiredConfigurationObservation != nil {
			return nil, fmt.Errorf("initial create forbids observations")
		}
		desired := request.Desired.Target
		return &ResourcePlanOperation{
			Decision: DecisionCreate,
			Desired:  &desired,
			Reasons:  []string{},
		}, nil
	}
	if err := request.Prior.Target.Validate(); err != nil {
		return nil, fmt.Errorf("prior target: %w", err)
	}
	bindingChanged := request.Desired != nil &&
		request.Desired.Target.Binding != request.Prior.Target.Binding
	if !bindingChanged {
		if request.Prior.Target.SchemaVersion < d.schemaVersion {
			return nil, fmt.Errorf(
				"resource schema version %d requires migration to %d",
				request.Prior.Target.SchemaVersion,
				d.schemaVersion,
			)
		}
		if request.Prior.Target.SchemaVersion > d.schemaVersion {
			return nil, fmt.Errorf(
				"recorded resource schema version %d is newer than registered version %d",
				request.Prior.Target.SchemaVersion,
				d.schemaVersion,
			)
		}
	}
	if request.RecordedObservation == nil {
		return nil, fmt.Errorf("resource with prior target requires recorded-target observation")
	}
	if err := request.RecordedObservation.Observation.Validate(); err != nil {
		return nil, fmt.Errorf("recorded-target observation: %w", err)
	}

	prior := request.Prior.Target
	recorded := request.RecordedObservation.Observation
	if request.Desired == nil {
		if request.DesiredConfigurationObservation != nil {
			return nil, fmt.Errorf("destroy forbids desired-configuration observation")
		}
		return &ResourcePlanOperation{
			Decision:    DecisionDestroy,
			Prior:       &prior,
			Observation: &recorded,
			Reasons:     []string{},
		}, nil
	}

	desired := request.Desired.Target
	if recorded.Status == ObservationAbsent {
		if request.DesiredConfigurationObservation != nil {
			return nil, fmt.Errorf("remote-missing create forbids desired-configuration observation")
		}
		return &ResourcePlanOperation{
			Decision:    DecisionCreate,
			Desired:     &desired,
			Prior:       &prior,
			Observation: &recorded,
			Reasons:     []string{"remote-missing"},
		}, nil
	}
	if err := validatePlanningIdentity(
		prior.Identity,
		recorded.Identity,
		"observed",
	); err != nil {
		return nil, err
	}
	if desired.Binding != prior.Binding {
		reasons := []string{"binding"}
		if desired.Configuration.Kind == PlannedConfigurationPending {
			reasons = append(reasons, "configuration-pending")
		}
		return existingResourceOperation(
			DecisionReplace,
			desired,
			prior,
			recorded,
			reasons,
		), nil
	}
	if _, err := d.currentIdentityRecord(
		prior.Binding.LibraryPath,
		prior.Binding.Export,
		prior.Identity,
	); err != nil {
		return nil, err
	}
	if desired.Configuration.Kind == PlannedConfigurationPending {
		return existingResourceOperation(
			DecisionReplace,
			desired,
			prior,
			recorded,
			[]string{"configuration-pending"},
		), nil
	}

	selected := request.RecordedObservation
	configurationChanged :=
		desired.Configuration.Record.Digest != prior.Configuration.Digest
	if configurationChanged {
		switch d.identityScope {
		case IdentityConfiguration:
			return existingResourceOperation(
				DecisionReplace,
				desired,
				prior,
				recorded,
				[]string{"configuration"},
			), nil
		case IdentityGlobal:
			if request.DesiredConfigurationObservation == nil {
				return nil, fmt.Errorf(
					"global configuration change requires desired observation",
				)
			}
			selected = request.DesiredConfigurationObservation
			if err := selected.Observation.Validate(); err != nil {
				return nil, fmt.Errorf("desired-configuration observation: %w", err)
			}
			if selected.Observation.Status == ObservationAbsent {
				return nil, fmt.Errorf("desired configuration read returned not found")
			}
			if err := validatePlanningIdentity(
				prior.Identity,
				selected.Observation.Identity,
				"desired-configuration",
			); err != nil {
				return nil, err
			}
		}
	} else if request.DesiredConfigurationObservation != nil {
		return nil, fmt.Errorf(
			"unchanged configuration forbids desired-configuration observation",
		)
	}

	reasons, inputsChanged, err := d.classifyInputChanges(
		request.Prior.Inputs,
		request.Desired.Inputs,
		prior.Inputs,
		desired.Inputs,
	)
	if err != nil {
		return nil, err
	}
	driftReasons, err := d.classifyDrift(
		request.Prior.Outputs,
		selected.Outputs,
		prior.Outputs,
		*selected.Observation.Outputs,
	)
	if err != nil {
		return nil, err
	}
	reasons = append(reasons, driftReasons...)
	slices.Sort(reasons)
	reasons = slices.Compact(reasons)

	observation := selected.Observation
	if len(reasons) > 0 {
		return existingResourceOperation(
			DecisionReplace,
			desired,
			prior,
			observation,
			reasons,
		), nil
	}
	if inputsChanged || !encodedValuesEqual(prior.Outputs, *observation.Outputs) {
		return existingResourceOperation(
			DecisionUpdate,
			desired,
			prior,
			observation,
			[]string{},
		), nil
	}
	return existingResourceOperation(
		DecisionNoOp,
		desired,
		prior,
		observation,
		[]string{},
	), nil
}

func existingResourceOperation(
	decision Decision,
	desired PlannedResourceTarget,
	prior ResourceTarget,
	observation ResourceObservation,
	reasons []string,
) *ResourcePlanOperation {
	return &ResourcePlanOperation{
		Decision:    decision,
		Desired:     &desired,
		Prior:       &prior,
		Observation: &observation,
		Reasons:     reasons,
	}
}

func validatePlanningIdentity(
	prior IdentityRecord,
	observed *IdentityRecord,
	label string,
) error {
	if observed == nil {
		return fmt.Errorf("%s observation requires identity", label)
	}
	if prior.DefinitionDigest != observed.DefinitionDigest || prior.Version != observed.Version {
		return fmt.Errorf("%s identity definition does not match recorded identity", label)
	}
	if !sameString(prior.StableID, observed.StableID) {
		return fmt.Errorf(
			"recorded stable ID %q does not match %s stable ID %q",
			stringValue(prior.StableID),
			label,
			stringValue(observed.StableID),
		)
	}
	return nil
}

type inputEquality struct {
	equal bool
}

func (d resolvedResourceDefinition[In, Out, Config]) classifyInputChanges(
	priorInputs In,
	desiredInputs In,
	priorEncoded EncodedValue,
	desiredEncoded EncodedValue,
) ([]string, bool, error) {
	equalities := make(map[string]inputEquality, len(d.inputRules))
	ignored := make(map[string]bool, len(d.inputRules))
	for _, rule := range d.inputRules {
		priorValue, desiredValue, err := encodedFieldPair(
			priorEncoded,
			desiredEncoded,
			rule.field.path,
		)
		if err != nil {
			return nil, false, err
		}
		equal := encodedValuesEqual(priorValue, desiredValue)
		if !equal && !desiredValue.HasPending() {
			semanticEqual, known, err := rule.equivalent(priorInputs, desiredInputs)
			if err != nil {
				return nil, false, err
			}
			if known {
				equal = semanticEqual
			}
		}
		equalities[rule.field.path] = inputEquality{equal: equal}
		ignored[rule.field.path] = true
	}

	reasons := make([]string, 0, len(d.addressInputs)+len(d.replacementInputs))
	for _, field := range d.addressInputs {
		priorValue, desiredValue, err := encodedFieldPair(
			priorEncoded,
			desiredEncoded,
			field.path,
		)
		if err != nil {
			return nil, false, err
		}
		if desiredValue.HasPending() || !encodedValuesEqual(priorValue, desiredValue) {
			reasons = append(reasons, "address:"+field.path)
		}
	}

	for _, rule := range d.replacementInputs {
		priorValue, desiredValue, err := encodedFieldPair(
			priorEncoded,
			desiredEncoded,
			rule.field.path,
		)
		if err != nil {
			return nil, false, err
		}
		if desiredValue.HasPending() {
			reasons = append(reasons, "input:"+rule.field.path)
			continue
		}
		equal := encodedValuesEqual(priorValue, desiredValue)
		if semantic, ok := equalities[rule.field.path]; ok {
			equal = semantic.equal
		}
		if equal {
			continue
		}
		matches, known, err := rule.matches(priorInputs, desiredInputs, false)
		if err != nil {
			return nil, false, err
		}
		if !known {
			return nil, false, fmt.Errorf(
				"input replacement field %q is unavailable",
				rule.field.path,
			)
		}
		if matches {
			reasons = append(reasons, "input:"+rule.field.path)
		}
	}

	inputsChanged := !encodedValuesEqualIgnoring(priorEncoded, desiredEncoded, ignored)
	for _, equality := range equalities {
		if !equality.equal {
			inputsChanged = true
			break
		}
	}
	return reasons, inputsChanged, nil
}

func (d resolvedResourceDefinition[In, Out, Config]) classifyDrift(
	recordedOutputs Out,
	observedOutputs Out,
	recordedEncoded EncodedValue,
	observedEncoded EncodedValue,
) ([]string, error) {
	reasons := make([]string, 0, len(d.driftRules))
	for _, rule := range d.driftRules {
		recordedValue, observedValue, err := encodedFieldPair(
			recordedEncoded,
			observedEncoded,
			rule.field.path,
		)
		if err != nil {
			return nil, err
		}
		if encodedValuesEqual(recordedValue, observedValue) {
			continue
		}
		matches, known, err := rule.matches(recordedOutputs, observedOutputs)
		if err != nil {
			return nil, err
		}
		if !known {
			return nil, fmt.Errorf(
				"drift field %q is unavailable",
				rule.field.path,
			)
		}
		if matches {
			reasons = append(reasons, "drift:"+rule.field.path)
		}
	}
	return reasons, nil
}

func encodedFieldPair(
	prior EncodedValue,
	desired EncodedValue,
	path string,
) (EncodedValue, EncodedValue, error) {
	priorValue, err := encodedField(prior, path)
	if err != nil {
		return EncodedValue{}, EncodedValue{}, fmt.Errorf("prior field %q: %w", path, err)
	}
	desiredValue, err := encodedField(desired, path)
	if err != nil {
		return EncodedValue{}, EncodedValue{}, fmt.Errorf("desired field %q: %w", path, err)
	}
	return priorValue, desiredValue, nil
}

func encodedField(value EncodedValue, path string) (EncodedValue, error) {
	current := value
	for segment := range strings.SplitSeq(path, ".") {
		if current.Kind() == EncodedValuePending {
			return current, nil
		}
		fields, ok := current.ObjectFields()
		if !ok {
			return EncodedValue{}, fmt.Errorf("cannot select %q from %s", segment, current.Kind())
		}
		next, ok := fields[segment]
		if !ok {
			return EncodedValue{}, fmt.Errorf("field %q is missing", segment)
		}
		current = next
	}
	return current, nil
}

func encodedValuesEqual(a, b EncodedValue) bool {
	return encodedValuesEqualAt(a, b, nil, nil)
}

func encodedValuesEqualIgnoring(
	a EncodedValue,
	b EncodedValue,
	ignored map[string]bool,
) bool {
	return encodedValuesEqualAt(a, b, nil, ignored)
}

func encodedValuesEqualAt(
	a EncodedValue,
	b EncodedValue,
	path []string,
	ignored map[string]bool,
) bool {
	if ignored[joinDescriptorPath(path)] {
		return true
	}
	if a.Kind() != b.Kind() {
		return false
	}
	switch a.Kind() {
	case EncodedValueAbsent, EncodedValueNull:
		return true
	case EncodedValueBoolean:
		av, _ := a.Boolean()
		bv, _ := b.Boolean()
		return av == bv
	case EncodedValueString:
		av, _ := a.String()
		bv, _ := b.String()
		return av == bv
	case EncodedValueInteger:
		av, _ := a.Integer()
		bv, _ := b.Integer()
		return av == bv
	case EncodedValueNumber:
		av, _ := a.Number()
		bv, _ := b.Number()
		return math.Float64bits(av) == math.Float64bits(bv)
	case EncodedValueList:
		av, _ := a.Items()
		bv, _ := b.Items()
		if len(av) != len(bv) {
			return false
		}
		for i := range av {
			if !encodedValuesEqualAt(av[i], bv[i], path, ignored) {
				return false
			}
		}
		return true
	case EncodedValueMap:
		av, _ := a.MapEntries()
		bv, _ := b.MapEntries()
		return encodedNamedValuesEqual(av, bv, path, ignored, false)
	case EncodedValueObject:
		av, _ := a.ObjectFields()
		bv, _ := b.ObjectFields()
		return encodedNamedValuesEqual(av, bv, path, ignored, true)
	case EncodedValuePending:
		av, _ := a.PendingRefs()
		bv, _ := b.PendingRefs()
		return slices.Equal(av, bv)
	default:
		return false
	}
}

func encodedNamedValuesEqual(
	a map[string]EncodedValue,
	b map[string]EncodedValue,
	path []string,
	ignored map[string]bool,
	extendPath bool,
) bool {
	if len(a) != len(b) {
		return false
	}
	for name, av := range a {
		bv, ok := b[name]
		if !ok {
			return false
		}
		nextPath := path
		if extendPath {
			nextPath = appendDescriptorPath(path, name)
		}
		if !encodedValuesEqualAt(av, bv, nextPath, ignored) {
			return false
		}
	}
	return true
}

func appendDescriptorPath(path []string, segment string) []string {
	result := make([]string, len(path)+1)
	copy(result, path)
	result[len(path)] = segment
	return result
}

func joinDescriptorPath(path []string) string {
	return strings.Join(path, ".")
}
