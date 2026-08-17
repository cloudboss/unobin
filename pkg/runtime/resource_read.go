package runtime

import (
	"context"
	"fmt"
	"reflect"
)

type resourceProviderReadFunc[In, Out, Config any] func(
	context.Context,
	In,
	Config,
	Out,
) (Out, error)

type resourceReaderPtr[In, Out, Config any] interface {
	*In
	Read(context.Context, Config, Out) (Out, error)
}

func newResourceProviderRead[
	In, Out, Config any,
	PT resourceReaderPtr[In, Out, Config],
](construct func() *In) resourceProviderReadFunc[In, Out, Config] {
	return func(
		ctx context.Context,
		inputs In,
		configuration Config,
		prior Out,
	) (Out, error) {
		var zero Out
		receiver, err := prepareResourceReceiver(construct, inputs)
		if err != nil {
			return zero, err
		}
		return PT(receiver).Read(ctx, configuration, prior)
	}
}

func prepareResourceReceiver[In any](construct func() *In, inputs In) (*In, error) {
	receiver := new(In)
	if construct != nil {
		receiver = construct()
		if receiver == nil {
			return nil, fmt.Errorf("resource constructor returned nil")
		}
	}
	fields, err := resourceStructFields(reflect.TypeFor[In]())
	if err != nil {
		return nil, err
	}
	source := reflect.ValueOf(inputs)
	target := reflect.ValueOf(receiver).Elem()
	for _, field := range fields {
		target.Field(field.index).Set(source.Field(field.index))
	}
	return receiver, nil
}

func (d resolvedResourceDefinition[In, Out, Config]) readResourceObservation(
	ctx context.Context,
	request resourceReadRequest,
	configuration Config,
	read resourceProviderReadFunc[In, Out, Config],
) (ResourceObservation, error) {
	if ctx == nil {
		return ResourceObservation{}, fmt.Errorf("read context is required")
	}
	if read == nil {
		return ResourceObservation{}, fmt.Errorf("resource read callback is required")
	}
	if err := request.validate(); err != nil {
		return ResourceObservation{}, err
	}
	inputs, err := decodeResourceInputs[In](request.Inputs)
	if err != nil {
		return ResourceObservation{}, fmt.Errorf("read resource inputs: %w", err)
	}
	prior, err := decodeResourceOutputs[Out](request.PriorOutputs)
	if err != nil {
		return ResourceObservation{}, fmt.Errorf("read resource prior outputs: %w", err)
	}
	outputs, err := guard(
		"reading this resource",
		false,
		func() (Out, error) {
			return read(ctx, inputs, configuration, prior)
		},
	)
	if err != nil {
		return ResourceObservation{}, fmt.Errorf("read resource: %w", err)
	}
	encoded, err := encodeResourceOutputs(outputs)
	if err != nil {
		return ResourceObservation{}, fmt.Errorf("read resource outputs: %w", err)
	}
	identity, err := d.newIdentityRecord(
		request.Binding.LibraryPath,
		request.Binding.Export,
		inputs,
		outputs,
	)
	if err != nil {
		return ResourceObservation{}, err
	}
	observation := ResourceObservation{
		Status:   ObservationPresent,
		Outputs:  &encoded,
		Identity: &identity,
	}
	if err := observation.Validate(); err != nil {
		return ResourceObservation{}, fmt.Errorf("resource observation: %w", err)
	}
	return observation, nil
}
