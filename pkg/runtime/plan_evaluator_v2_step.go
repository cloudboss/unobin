package runtime

import "fmt"

func publishPlanEvaluationV2Outputs(
	values map[string]any,
	address string,
	outputs *EncodedValue,
) error {
	template, key := splitInstanceAddress(address)
	if outputs != nil {
		fields, _ := outputs.ObjectFields()
		decoded, err := decodeConcreteObjectFields(fields, "planning output")
		if err != nil {
			return fmt.Errorf("%s: %w", address, err)
		}
		if key == "" {
			seedAddress(values, template, decoded)
		} else {
			seedAddressInstance(values, template, key, decoded)
		}
		return nil
	}
	path, _ := addressValuePath(template)
	if key != "" {
		path = append(path, key)
	}
	for _, name := range path[:len(path)-1] {
		nested, ok := values[name].(map[string]any)
		if !ok {
			return nil
		}
		values = nested
	}
	delete(values, path[len(path)-1])
	return nil
}
