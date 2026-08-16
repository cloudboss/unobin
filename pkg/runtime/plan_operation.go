package runtime

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	internalconfig "github.com/cloudboss/unobin/internal/configuration"
	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/cloudboss/unobin/pkg/stateref"
)

const PlanFormatVersionV2 = 2

type Binding = state.CanonicalBinding
type ResourceTarget = state.ResourceTarget
type ResourceStatePayload = state.ResourceStatePayload
type ActionStatePayload = state.ActionStatePayload
type DataSourceStatePayload = state.DataSourceStatePayload
type CompositeStatePayload = state.CompositeStatePayload

type ObservationStatus string

const (
	ObservationPresent ObservationStatus = "present"
	ObservationAbsent  ObservationStatus = "absent"
)

type ResourceObservation struct {
	Status   ObservationStatus `json:"status"`
	Outputs  *EncodedValue     `json:"outputs,omitempty"`
	Identity *IdentityRecord   `json:"identity,omitempty"`
}

func (o ResourceObservation) Validate() error {
	switch o.Status {
	case ObservationPresent:
		if o.Outputs == nil {
			return fmt.Errorf("present observation requires outputs")
		}
		if o.Identity == nil {
			return fmt.Errorf("present observation requires identity")
		}
		if err := validatePlanObject(*o.Outputs, "observation outputs", false); err != nil {
			return err
		}
		if err := o.Identity.Validate(); err != nil {
			return fmt.Errorf("observation identity: %w", err)
		}
		return nil
	case ObservationAbsent:
		if o.Outputs != nil || o.Identity != nil {
			return fmt.Errorf("absent observation forbids outputs and identity")
		}
		return nil
	default:
		return fmt.Errorf("unknown observation status %q", o.Status)
	}
}

type PlannedResourceTarget struct {
	Binding              Binding              `json:"binding"`
	Inputs               EncodedValue         `json:"inputs"`
	Configuration        PlannedConfiguration `json:"configuration"`
	SensitiveInputPaths  []string             `json:"sensitive-input-paths"`
	SensitiveOutputPaths []string             `json:"sensitive-output-paths"`
}

func (t PlannedResourceTarget) Validate() error {
	return validatePlannedProviderTarget(
		t.Binding,
		t.Inputs,
		t.Configuration,
		t.SensitiveInputPaths,
		t.SensitiveOutputPaths,
	)
}

type ResourcePlanOperation struct {
	Decision    Decision               `json:"decision"`
	Desired     *PlannedResourceTarget `json:"desired,omitempty"`
	Prior       *ResourceTarget        `json:"prior,omitempty"`
	Observation *ResourceObservation   `json:"observation,omitempty"`
	Reasons     []string               `json:"reasons"`
}

func (o ResourcePlanOperation) Validate() error {
	switch o.Decision {
	case DecisionCreate, DecisionUpdate, DecisionReplace, DecisionDestroy, DecisionNoOp:
	default:
		return fmt.Errorf("resource decision is invalid: %q", o.Decision)
	}
	if o.Reasons == nil {
		return fmt.Errorf("reasons are required")
	}
	switch o.Decision {
	case DecisionCreate:
		if o.Desired == nil {
			return fmt.Errorf("create requires desired target")
		}
		remoteMissing := len(o.Reasons) == 1 && o.Reasons[0] == "remote-missing"
		if remoteMissing {
			if o.Prior == nil {
				return fmt.Errorf("remote-missing create requires prior target")
			}
			if o.Observation == nil || o.Observation.Status != ObservationAbsent {
				return fmt.Errorf("remote-missing create requires an absent observation")
			}
		} else {
			if len(o.Reasons) != 0 {
				return fmt.Errorf("initial create forbids reasons")
			}
			if o.Prior != nil || o.Observation != nil {
				return fmt.Errorf("initial create forbids prior target and observation")
			}
		}
	case DecisionUpdate:
		if err := o.requireExisting("update", true); err != nil {
			return err
		}
		if len(o.Reasons) != 0 {
			return fmt.Errorf("update forbids reasons")
		}
	case DecisionReplace:
		if err := o.requireExisting("replace", true); err != nil {
			return err
		}
		if len(o.Reasons) == 0 {
			return fmt.Errorf("replace requires at least one reason")
		}
		if err := validateReplacementReasons(o.Reasons); err != nil {
			return err
		}
	case DecisionDestroy:
		if o.Desired != nil {
			return fmt.Errorf("destroy forbids desired target")
		}
		if o.Prior == nil {
			return fmt.Errorf("destroy requires prior target")
		}
		if o.Observation == nil {
			return fmt.Errorf("destroy requires observation")
		}
		if len(o.Reasons) != 0 {
			return fmt.Errorf("destroy forbids reasons")
		}
	case DecisionNoOp:
		if err := o.requireExisting("no-op", true); err != nil {
			return err
		}
		if len(o.Reasons) != 0 {
			return fmt.Errorf("no-op forbids reasons")
		}
	}

	if o.Desired != nil {
		if err := o.Desired.Validate(); err != nil {
			return fmt.Errorf("desired target: %w", err)
		}
	}
	if o.Prior != nil {
		if err := o.Prior.Validate(); err != nil {
			return fmt.Errorf("prior target: %w", err)
		}
	}
	if o.Observation != nil {
		if err := o.Observation.Validate(); err != nil {
			return fmt.Errorf("observation: %w", err)
		}
	}
	if o.Decision == DecisionUpdate || o.Decision == DecisionNoOp {
		if o.Desired.Configuration.Kind != PlannedConfigurationConcrete {
			return fmt.Errorf("%s requires concrete desired configuration", o.Decision)
		}
	}
	if o.Decision == DecisionReplace {
		pendingReason := slices.Contains(o.Reasons, "configuration-pending")
		pendingConfiguration := o.Desired.Configuration.Kind == PlannedConfigurationPending
		switch {
		case pendingConfiguration && !pendingReason:
			return fmt.Errorf(
				"pending replacement configuration requires configuration-pending reason",
			)
		case pendingReason && !pendingConfiguration:
			return fmt.Errorf(
				"configuration-pending reason requires pending configuration",
			)
		}
	}
	return nil
}

func (o ResourcePlanOperation) requireExisting(subject string, desired bool) error {
	if desired && o.Desired == nil {
		return fmt.Errorf("%s requires desired target", subject)
	}
	if o.Prior == nil {
		return fmt.Errorf("%s requires prior target", subject)
	}
	if o.Observation == nil || o.Observation.Status != ObservationPresent {
		return fmt.Errorf("%s requires a present observation", subject)
	}
	return nil
}

type PlannedActionTarget struct {
	Binding              Binding              `json:"binding"`
	Inputs               EncodedValue         `json:"inputs"`
	Configuration        PlannedConfiguration `json:"configuration"`
	TriggerHash          string               `json:"trigger-hash"`
	SensitiveInputPaths  []string             `json:"sensitive-input-paths"`
	SensitiveOutputPaths []string             `json:"sensitive-output-paths"`
}

func (t PlannedActionTarget) Validate() error {
	return validatePlannedProviderTarget(
		t.Binding,
		t.Inputs,
		t.Configuration,
		t.SensitiveInputPaths,
		t.SensitiveOutputPaths,
	)
}

type ActionPlanOperation struct {
	Decision Decision             `json:"decision"`
	Desired  *PlannedActionTarget `json:"desired,omitempty"`
	Prior    *ActionStatePayload  `json:"prior,omitempty"`
}

func (o ActionPlanOperation) Validate() error {
	switch o.Decision {
	case DecisionRerun:
		if o.Desired == nil {
			return fmt.Errorf("rerun requires desired target")
		}
	case DecisionSkip:
		if o.Desired == nil {
			return fmt.Errorf("skip requires desired target")
		}
		if o.Prior == nil {
			return fmt.Errorf("skip requires prior state")
		}
	case DecisionDestroy:
		if o.Desired != nil {
			return fmt.Errorf("destroy forbids desired target")
		}
		if o.Prior == nil {
			return fmt.Errorf("destroy requires prior state")
		}
	default:
		return fmt.Errorf("action decision is invalid: %q", o.Decision)
	}
	if o.Desired != nil {
		if err := o.Desired.Validate(); err != nil {
			return fmt.Errorf("desired target: %w", err)
		}
	}
	if o.Prior != nil {
		if err := o.Prior.Validate(); err != nil {
			return fmt.Errorf("prior state: %w", err)
		}
	}
	if o.Decision == DecisionSkip {
		if o.Desired.Inputs.HasPending() ||
			o.Desired.Configuration.Kind != PlannedConfigurationConcrete {
			return fmt.Errorf("skip requires concrete desired target")
		}
		if o.Desired.TriggerHash == "" || o.Desired.TriggerHash != o.Prior.TriggerHash {
			return fmt.Errorf("skip requires equal non-empty trigger hashes")
		}
	}
	return nil
}

type PlannedDataSourceTarget struct {
	Binding              Binding              `json:"binding"`
	Inputs               EncodedValue         `json:"inputs"`
	Configuration        PlannedConfiguration `json:"configuration"`
	SensitiveInputPaths  []string             `json:"sensitive-input-paths"`
	SensitiveOutputPaths []string             `json:"sensitive-output-paths"`
}

func (t PlannedDataSourceTarget) Validate() error {
	return validatePlannedProviderTarget(
		t.Binding,
		t.Inputs,
		t.Configuration,
		t.SensitiveInputPaths,
		t.SensitiveOutputPaths,
	)
}

type DataSourcePlanOperation struct {
	Decision        Decision                 `json:"decision"`
	Desired         *PlannedDataSourceTarget `json:"desired,omitempty"`
	Prior           *DataSourceStatePayload  `json:"prior,omitempty"`
	ObservedOutputs *EncodedValue            `json:"observed-outputs,omitempty"`
}

func (o DataSourcePlanOperation) Validate() error {
	switch o.Decision {
	case DecisionRead:
		if o.Desired == nil {
			return fmt.Errorf("read requires desired target")
		}
	case DecisionDestroy:
		if o.Desired != nil {
			return fmt.Errorf("destroy forbids desired target")
		}
		if o.Prior == nil {
			return fmt.Errorf("destroy requires prior state")
		}
		if o.ObservedOutputs != nil {
			return fmt.Errorf("destroy forbids observed outputs")
		}
	default:
		return fmt.Errorf("data-source decision is invalid: %q", o.Decision)
	}
	if o.Desired != nil {
		if err := o.Desired.Validate(); err != nil {
			return fmt.Errorf("desired target: %w", err)
		}
	}
	if o.Prior != nil {
		if err := o.Prior.Validate(); err != nil {
			return fmt.Errorf("prior state: %w", err)
		}
	}
	if o.Decision == DecisionRead {
		concrete := !o.Desired.Inputs.HasPending() &&
			o.Desired.Configuration.Kind == PlannedConfigurationConcrete
		if concrete && o.ObservedOutputs == nil {
			return fmt.Errorf("concrete read requires observed outputs")
		}
		if !concrete && o.ObservedOutputs != nil {
			return fmt.Errorf("pending read forbids observed outputs")
		}
	}
	if o.ObservedOutputs != nil {
		if err := validatePlanObject(*o.ObservedOutputs, "observed outputs", false); err != nil {
			return err
		}
	}
	return nil
}

type LibraryConfigurationPlanOperation struct {
	Decision Decision             `json:"decision"`
	Inputs   EncodedValue         `json:"inputs"`
	Result   PlannedConfiguration `json:"result"`
}

func (o LibraryConfigurationPlanOperation) Validate() error {
	if o.Decision != DecisionEval {
		return fmt.Errorf("library-configuration decision must be eval")
	}
	if err := validatePlanObject(o.Inputs, "inputs", true); err != nil {
		return err
	}
	if err := o.Result.Validate(); err != nil {
		return fmt.Errorf("result: %w", err)
	}
	refs := pendingReferences(o.Inputs)
	if len(refs) == 0 {
		if o.Result.Kind != PlannedConfigurationConcrete {
			return fmt.Errorf("concrete inputs require concrete result")
		}
		return nil
	}
	if o.Result.Kind != PlannedConfigurationPending {
		return fmt.Errorf("pending inputs require pending result")
	}
	if !slices.Equal(refs, o.Result.PendingRefs) {
		return fmt.Errorf("pending result references do not match inputs")
	}
	return nil
}

type PlannedCompositeTarget struct {
	Category             NodeKind     `json:"category"`
	Binding              Binding      `json:"binding"`
	Inputs               EncodedValue `json:"inputs"`
	SensitiveInputPaths  []string     `json:"sensitive-input-paths"`
	SensitiveOutputPaths []string     `json:"sensitive-output-paths"`
}

func (t PlannedCompositeTarget) Validate() error {
	if !validCompositeCategory(t.Category) {
		return fmt.Errorf("category is invalid: %q", t.Category)
	}
	if err := t.Binding.Validate(); err != nil {
		return fmt.Errorf("binding: %w", err)
	}
	if err := validatePlanObject(t.Inputs, "inputs", true); err != nil {
		return err
	}
	return validatePlannedPaths(
		t.Inputs,
		t.SensitiveInputPaths,
		t.SensitiveOutputPaths,
	)
}

type CompositePlanOperation struct {
	Decision Decision                `json:"decision"`
	Desired  *PlannedCompositeTarget `json:"desired,omitempty"`
	Prior    *CompositeStatePayload  `json:"prior,omitempty"`
}

func (o CompositePlanOperation) Validate(category NodeKind) error {
	if !validCompositeCategory(category) {
		return fmt.Errorf("composite step category is invalid: %q", category)
	}
	switch o.Decision {
	case DecisionEval:
		if o.Desired == nil {
			return fmt.Errorf("eval requires desired target")
		}
		if o.Desired.Category != category {
			return fmt.Errorf(
				"desired category %s does not match %s",
				o.Desired.Category,
				category,
			)
		}
	case DecisionDestroy:
		if o.Desired != nil {
			return fmt.Errorf("destroy forbids desired target")
		}
		if o.Prior == nil {
			return fmt.Errorf("destroy requires prior state")
		}
		if NodeKind(o.Prior.Category) != category {
			return fmt.Errorf(
				"prior category %s does not match %s",
				o.Prior.Category,
				category,
			)
		}
	default:
		return fmt.Errorf("composite decision is invalid: %q", o.Decision)
	}
	if o.Desired != nil {
		if err := o.Desired.Validate(); err != nil {
			return fmt.Errorf("desired target: %w", err)
		}
	}
	if o.Prior != nil {
		if err := o.Prior.Validate(); err != nil {
			return fmt.Errorf("prior state: %w", err)
		}
	}
	return nil
}

type OutputPlanOperation struct {
	Decision  Decision     `json:"decision"`
	Value     EncodedValue `json:"value"`
	Sensitive bool         `json:"sensitive"`
}

func (o OutputPlanOperation) Validate() error {
	if o.Decision != DecisionEval {
		return fmt.Errorf("output decision must be eval")
	}
	return validatePlanValue(o.Value, "output value", true)
}

type StepOperationKind string

const (
	StepResource             StepOperationKind = "resource"
	StepAction               StepOperationKind = "action"
	StepDataSource           StepOperationKind = "data-source"
	StepLibraryConfiguration StepOperationKind = "library-configuration"
	StepComposite            StepOperationKind = "composite"
	StepOutput               StepOperationKind = "output"
)

type StepOperation struct {
	Kind                 StepOperationKind                  `json:"kind"`
	Resource             *ResourcePlanOperation             `json:"resource,omitempty"`
	Action               *ActionPlanOperation               `json:"action,omitempty"`
	DataSource           *DataSourcePlanOperation           `json:"data-source,omitempty"`
	LibraryConfiguration *LibraryConfigurationPlanOperation `json:"library-configuration,omitempty"`
	Composite            *CompositePlanOperation            `json:"composite,omitempty"`
	Output               *OutputPlanOperation               `json:"output,omitempty"`
}

func (o StepOperation) Validate(kind NodeKind) error {
	switch o.Kind {
	case StepResource:
		if kind != NodeResource {
			return fmt.Errorf("resource operation does not match %s step", kind)
		}
		if o.Resource == nil {
			return fmt.Errorf("resource operation payload is required")
		}
		if o.payloadCount() != 1 {
			return fmt.Errorf("resource operation forbids other payloads")
		}
		return o.Resource.Validate()
	case StepAction:
		if kind != NodeAction {
			return fmt.Errorf("action operation does not match %s step", kind)
		}
		if o.Action == nil {
			return fmt.Errorf("action operation payload is required")
		}
		if o.payloadCount() != 1 {
			return fmt.Errorf("action operation forbids other payloads")
		}
		return o.Action.Validate()
	case StepDataSource:
		if kind != NodeDataSource {
			return fmt.Errorf("data-source operation does not match %s step", kind)
		}
		if o.DataSource == nil {
			return fmt.Errorf("data-source operation payload is required")
		}
		if o.payloadCount() != 1 {
			return fmt.Errorf("data-source operation forbids other payloads")
		}
		return o.DataSource.Validate()
	case StepLibraryConfiguration:
		if kind != NodeLibraryConfiguration {
			return fmt.Errorf("library-configuration operation does not match %s step", kind)
		}
		if o.LibraryConfiguration == nil {
			return fmt.Errorf("library-configuration operation payload is required")
		}
		if o.payloadCount() != 1 {
			return fmt.Errorf("library-configuration operation forbids other payloads")
		}
		return o.LibraryConfiguration.Validate()
	case StepComposite:
		if !validCompositeCategory(kind) {
			return fmt.Errorf("composite operation does not match %s step", kind)
		}
		if o.Composite == nil {
			return fmt.Errorf("composite operation payload is required")
		}
		if o.payloadCount() != 1 {
			return fmt.Errorf("composite operation forbids other payloads")
		}
		return o.Composite.Validate(kind)
	case StepOutput:
		if kind != NodeOutput {
			return fmt.Errorf("output operation does not match %s step", kind)
		}
		if o.Output == nil {
			return fmt.Errorf("output operation payload is required")
		}
		if o.payloadCount() != 1 {
			return fmt.Errorf("output operation forbids other payloads")
		}
		return o.Output.Validate()
	default:
		return fmt.Errorf("unknown step operation kind %q", o.Kind)
	}
}

func (o StepOperation) payloadCount() int {
	count := 0
	for _, present := range []bool{
		o.Resource != nil,
		o.Action != nil,
		o.DataSource != nil,
		o.LibraryConfiguration != nil,
		o.Composite != nil,
		o.Output != nil,
	} {
		if present {
			count++
		}
	}
	return count
}

type PlanStepV2 struct {
	Address   string        `json:"address"`
	Kind      NodeKind      `json:"kind"`
	DependsOn []string      `json:"depends-on"`
	Operation StepOperation `json:"operation"`
}

func (s PlanStepV2) Validate() error {
	if err := validateNodeAddress(s.Address, s.Kind); err != nil {
		return err
	}
	if err := validatePlanDependencies(s.DependsOn); err != nil {
		return err
	}
	if err := s.Operation.Validate(s.Kind); err != nil {
		return fmt.Errorf("operation: %w", err)
	}
	return nil
}

type PlanMode string

const (
	PlanApply   PlanMode = "apply"
	PlanDestroy PlanMode = "destroy"
)

type StateRefV2 struct {
	Name string       `json:"name"`
	Body EncodedValue `json:"body"`
}

func (r StateRefV2) Validate() error {
	if r.Name == "" {
		return fmt.Errorf("state backend name is required")
	}
	return validatePlanObject(r.Body, "state backend body", false)
}

type PlanFileV2 struct {
	FormatVersion int                `json:"format-version"`
	Factory       FactoryRef         `json:"factory"`
	Stack         string             `json:"stack"`
	StateRevision string             `json:"state-revision"`
	GeneratedAt   time.Time          `json:"generated-at"`
	Inputs        EncodedValue       `json:"inputs"`
	Backend       *StateRefV2        `json:"backend,omitempty"`
	Parallelism   int                `json:"parallelism"`
	Mode          PlanMode           `json:"mode"`
	StateMoves    []PlannedEntryMove `json:"state-moves"`
	Steps         []PlanStepV2       `json:"steps"`
	Digest        string             `json:"digest"`
}

func (p PlanFileV2) Validate() error {
	if p.FormatVersion != PlanFormatVersionV2 {
		return fmt.Errorf("format version must be %d", PlanFormatVersionV2)
	}
	if p.Factory.Name == "" {
		return fmt.Errorf("factory name is required")
	}
	if p.Factory.Version == "" {
		return fmt.Errorf("factory version is required")
	}
	if p.Factory.ContentRevision == "" {
		return fmt.Errorf("factory content revision is required")
	}
	if p.Stack == "" {
		return fmt.Errorf("stack is required")
	}
	if p.GeneratedAt.IsZero() {
		return fmt.Errorf("generated time is required")
	}
	_, offset := p.GeneratedAt.Zone()
	if offset != 0 {
		return fmt.Errorf("generated time must use UTC")
	}
	if err := validatePlanObject(p.Inputs, "inputs", false); err != nil {
		return err
	}
	if p.Backend != nil {
		if err := p.Backend.Validate(); err != nil {
			return fmt.Errorf("backend: %w", err)
		}
	}
	switch p.Mode {
	case PlanApply, PlanDestroy:
	default:
		return fmt.Errorf("plan mode is invalid: %q", p.Mode)
	}
	if p.StateMoves == nil {
		return fmt.Errorf("state moves are required")
	}
	if err := validatePlanMoves(p.StateMoves); err != nil {
		return err
	}
	if p.Steps == nil {
		return fmt.Errorf("steps are required")
	}
	addresses := make(map[string]bool, len(p.Steps))
	for i := range p.Steps {
		if addresses[p.Steps[i].Address] {
			return fmt.Errorf("steps contain duplicate address %q", p.Steps[i].Address)
		}
		addresses[p.Steps[i].Address] = true
		if err := p.Steps[i].Validate(); err != nil {
			return fmt.Errorf("steps[%d]: %w", i, err)
		}
	}
	if !isLowerSHA256(p.Digest) {
		return fmt.Errorf("digest must be a lowercase SHA-256 digest")
	}
	digest, err := planFileV2Digest(p)
	if err != nil {
		return err
	}
	if p.Digest != digest {
		return fmt.Errorf("digest does not match plan contents")
	}
	return nil
}

func planFileV2Digest(p PlanFileV2) (string, error) {
	payload := struct {
		FormatVersion int                `json:"format-version"`
		Factory       FactoryRef         `json:"factory"`
		Stack         string             `json:"stack"`
		StateRevision string             `json:"state-revision"`
		GeneratedAt   time.Time          `json:"generated-at"`
		Inputs        EncodedValue       `json:"inputs"`
		Backend       *StateRefV2        `json:"backend,omitempty"`
		Parallelism   int                `json:"parallelism"`
		Mode          PlanMode           `json:"mode"`
		StateMoves    []PlannedEntryMove `json:"state-moves"`
		Steps         []PlanStepV2       `json:"steps"`
	}{
		FormatVersion: p.FormatVersion,
		Factory:       p.Factory,
		Stack:         p.Stack,
		StateRevision: p.StateRevision,
		GeneratedAt:   p.GeneratedAt,
		Inputs:        p.Inputs,
		Backend:       p.Backend,
		Parallelism:   p.Parallelism,
		Mode:          p.Mode,
		StateMoves:    p.StateMoves,
		Steps:         p.Steps,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode plan digest: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func validatePlannedProviderTarget(
	binding Binding,
	inputs EncodedValue,
	configuration PlannedConfiguration,
	sensitiveInputPaths []string,
	sensitiveOutputPaths []string,
) error {
	if err := binding.Validate(); err != nil {
		return fmt.Errorf("binding: %w", err)
	}
	if err := validatePlanObject(inputs, "inputs", true); err != nil {
		return err
	}
	if err := configuration.Validate(); err != nil {
		return fmt.Errorf("configuration: %w", err)
	}
	if configuration.Record != nil && configuration.Record.LibraryPath != binding.LibraryPath {
		return fmt.Errorf("configuration library path does not match binding")
	}
	return validatePlannedPaths(inputs, sensitiveInputPaths, sensitiveOutputPaths)
}

func validatePlannedPaths(
	inputs EncodedValue,
	sensitiveInputPaths []string,
	sensitiveOutputPaths []string,
) error {
	if sensitiveInputPaths == nil {
		return fmt.Errorf("sensitive input paths are required")
	}
	var err error
	if inputs.HasPending() {
		err = internalconfig.ValidatePathSyntax(sensitiveInputPaths, "sensitive input")
	} else {
		err = internalconfig.ValidatePaths(inputs, sensitiveInputPaths, "sensitive input")
	}
	if err != nil {
		return err
	}
	if sensitiveOutputPaths == nil {
		return fmt.Errorf("sensitive output paths are required")
	}
	return internalconfig.ValidatePathSyntax(sensitiveOutputPaths, "sensitive output")
}

func validatePlanValue(value EncodedValue, subject string, allowPending bool) error {
	if _, err := json.Marshal(value); err != nil {
		return fmt.Errorf("%s is invalid: %w", subject, err)
	}
	if !allowPending && value.HasPending() {
		return fmt.Errorf("%s must be concrete", subject)
	}
	return nil
}

func validatePlanObject(value EncodedValue, subject string, allowPending bool) error {
	if err := validatePlanValue(value, subject, allowPending); err != nil {
		return err
	}
	if _, ok := value.ObjectFields(); !ok {
		return fmt.Errorf("%s must be an object", subject)
	}
	return nil
}

func validateReplacementReasons(reasons []string) error {
	for i, reason := range reasons {
		if i > 0 && reason <= reasons[i-1] {
			return fmt.Errorf("reasons must be unique and sorted")
		}
		if !validReplacementReason(reason) {
			return fmt.Errorf("replacement reason is invalid: %q", reason)
		}
	}
	return nil
}

func validReplacementReason(reason string) bool {
	switch reason {
	case "binding", "configuration", "configuration-pending":
		return true
	}
	for _, prefix := range []string{"address:", "input:", "drift:"} {
		if after, ok := strings.CutPrefix(reason, prefix); ok {
			return validDescriptorPath(after)
		}
	}
	return false
}

func validDescriptorPath(path string) bool {
	if path == "" {
		return false
	}
	for segment := range strings.SplitSeq(path, ".") {
		if !validPlanName(segment) {
			return false
		}
	}
	return true
}

func pendingReferences(value EncodedValue) []string {
	refs := map[string]bool{}
	collectPendingReferences(value, refs)
	result := make([]string, 0, len(refs))
	for ref := range refs {
		result = append(result, ref)
	}
	slices.Sort(result)
	return result
}

func collectPendingReferences(value EncodedValue, refs map[string]bool) {
	if pending, ok := value.PendingRefs(); ok {
		for _, ref := range pending {
			refs[ref] = true
		}
		return
	}
	if items, ok := value.Items(); ok {
		for _, item := range items {
			collectPendingReferences(item, refs)
		}
		return
	}
	if entries, ok := value.MapEntries(); ok {
		for _, item := range entries {
			collectPendingReferences(item, refs)
		}
		return
	}
	if fields, ok := value.ObjectFields(); ok {
		for _, item := range fields {
			collectPendingReferences(item, refs)
		}
	}
}

func validCompositeCategory(category NodeKind) bool {
	switch category {
	case NodeResource, NodeAction, NodeDataSource:
		return true
	default:
		return false
	}
}

func validateNodeAddress(address string, kind NodeKind) error {
	switch kind {
	case NodeResource, NodeAction, NodeDataSource:
		ref, err := stateref.ParseStateRef(address)
		if err != nil {
			return fmt.Errorf("address is invalid: %w", err)
		}
		category := NodeKind(ref.Segments[len(ref.Segments)-1].Category)
		if category != kind {
			return fmt.Errorf("address category %s does not match %s", category, kind)
		}
		return nil
	case NodeLibraryConfiguration:
		if !validSpecialNodeAddress(address, "library-config") {
			return fmt.Errorf("library-configuration address is invalid: %q", address)
		}
		return nil
	case NodeOutput:
		if !validSpecialNodeAddress(address, "output") || strings.Contains(address, "/") {
			return fmt.Errorf("output address is invalid: %q", address)
		}
		return nil
	default:
		return fmt.Errorf("node kind is invalid: %q", kind)
	}
}

func validSpecialNodeAddress(address, prefix string) bool {
	index := strings.LastIndexByte(address, '/')
	if index >= 0 {
		if _, err := stateref.ParseStateRef(address[:index]); err != nil {
			return false
		}
		address = address[index+1:]
	}
	name, ok := strings.CutPrefix(address, prefix+".")
	return ok && validPlanName(name)
}

func validPlanName(value string) bool {
	if value == "" || !planNameLetter(value[0]) {
		return false
	}
	for i := 1; i < len(value); i++ {
		char := value[i]
		if planNameLetter(char) || char >= '0' && char <= '9' || char == '-' {
			continue
		}
		return false
	}
	last := value[len(value)-1]
	return planNameLetter(last) || last >= '0' && last <= '9'
}

func planNameLetter(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z'
}

func validatePlanDependencies(dependencies []string) error {
	if dependencies == nil {
		return fmt.Errorf("dependencies are required")
	}
	for i, dependency := range dependencies {
		if i > 0 && dependency <= dependencies[i-1] {
			return fmt.Errorf("dependencies must be unique and sorted")
		}
		if !validAnyPlanAddress(dependency) {
			return fmt.Errorf("dependency is invalid: %q", dependency)
		}
	}
	return nil
}

func validAnyPlanAddress(address string) bool {
	if _, err := stateref.ParseStateRef(address); err == nil {
		return true
	}
	return validSpecialNodeAddress(address, "library-config")
}

func validatePlanMoves(moves []PlannedEntryMove) error {
	sources := map[string]bool{}
	destinations := map[string]bool{}
	for i, move := range moves {
		if err := stateref.ValidateAddress(move.From); err != nil {
			return fmt.Errorf("state move %d source is invalid", i)
		}
		if err := stateref.ValidateAddress(move.To); err != nil {
			return fmt.Errorf("state move %d destination is invalid", i)
		}
		if move.From == move.To {
			return fmt.Errorf("state move %d endpoints must differ", i)
		}
		if sources[move.From] {
			return fmt.Errorf("state move source %q is duplicated", move.From)
		}
		if destinations[move.To] {
			return fmt.Errorf("state move destination %q is duplicated", move.To)
		}
		sources[move.From] = true
		destinations[move.To] = true
	}
	return nil
}
