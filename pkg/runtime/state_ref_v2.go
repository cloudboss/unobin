package runtime

// NewStateRefV2 encodes concrete state-provider arguments for a saved plan.
func NewStateRefV2(name string, body map[string]any) (*StateRefV2, error) {
	encoded, err := encodeFactoryApplyV2Object(body)
	if err != nil {
		return nil, err
	}
	ref := &StateRefV2{Name: name, Body: encoded}
	if err := ref.Validate(); err != nil {
		return nil, err
	}
	return ref, nil
}

// Values returns a detached copy of the state-provider arguments.
func (r StateRefV2) Values() (map[string]any, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	fields, _ := r.Body.ObjectFields()
	return decodeConcreteObjectFields(fields, "state provider")
}
