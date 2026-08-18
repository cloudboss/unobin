package runtime

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/cloudboss/unobin/internal/strictjson"
)

func encodePlanFileV2(plan PlanFileV2) ([]byte, error) {
	if err := plan.Validate(); err != nil {
		return nil, fmt.Errorf("plan: %w", err)
	}
	encoded, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode plan: %w", err)
	}
	return append(encoded, '\n'), nil
}

func decodePlanFileV2(data []byte) (PlanFileV2, error) {
	if err := strictjson.Validate(data); err != nil {
		return PlanFileV2{}, fmt.Errorf("plan: %w", err)
	}
	var header struct {
		FormatVersion int `json:"format-version"`
	}
	if err := json.NewDecoder(bytes.NewReader(data)).Decode(&header); err == nil &&
		header.FormatVersion == 1 {
		return PlanFileV2{}, fmt.Errorf(
			"plan: obsolete alpha format; create a new plan or state",
		)
	}

	var plan PlanFileV2
	if err := strictjson.Decode(data, &plan); err != nil {
		return PlanFileV2{}, fmt.Errorf("plan: %w", err)
	}
	if err := plan.Validate(); err != nil {
		return PlanFileV2{}, fmt.Errorf("plan: %w", err)
	}
	return plan, nil
}

func finalizePlanFileV2(plan PlanFileV2) (PlanFileV2, error) {
	plan.FormatVersion = PlanFormatVersionV2
	plan.Digest = ""

	encoded, err := json.Marshal(plan)
	if err != nil {
		return PlanFileV2{}, fmt.Errorf("copy plan contents: %w", err)
	}
	var finalized PlanFileV2
	if err := json.Unmarshal(encoded, &finalized); err != nil {
		return PlanFileV2{}, fmt.Errorf("copy plan contents: %w", err)
	}
	finalized.Digest, err = planFileV2Digest(finalized)
	if err != nil {
		return PlanFileV2{}, err
	}
	if err := finalized.Validate(); err != nil {
		return PlanFileV2{}, fmt.Errorf("plan: %w", err)
	}
	return finalized, nil
}
