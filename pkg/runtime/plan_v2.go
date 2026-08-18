package runtime

import (
	"encoding/json"
	"fmt"
)

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
