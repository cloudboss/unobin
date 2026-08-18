package runtime

import "context"

type resourceProviderCreateFunc[In, Out, Config any] func(
	context.Context,
	In,
	Config,
) (preparedResourceApplyResult[Out], error)

type resourceProviderUpdateFunc[In, Out, Config any] func(
	context.Context,
	In,
	Config,
	Prior[In, Out],
) (preparedResourceApplyResult[Out], error)

type resourceProviderDeleteFunc[In, Out, Config any] func(
	context.Context,
	In,
	Config,
	EncodedValue,
) error

type resourceCreatorPtr[In, Out, Config any] interface {
	*In
	Create(context.Context, Config) (Out, error)
}

type resourceUpdaterPtr[In, Out, Config any] interface {
	*In
	Update(context.Context, Config, Prior[In, Out]) (Out, error)
}

type resourceDeleterPtr[In, Out, Config any] interface {
	*In
	Delete(context.Context, Config, Out) error
}

func newResourceProviderCreate[
	In, Out, Config any,
	PT resourceCreatorPtr[In, Out, Config],
](construct func() *In) resourceProviderCreateFunc[In, Out, Config] {
	return func(
		ctx context.Context,
		inputs In,
		configuration Config,
	) (preparedResourceApplyResult[Out], error) {
		var zero preparedResourceApplyResult[Out]
		receiver, err := prepareResourceReceiver(construct, inputs)
		if err != nil {
			return zero, err
		}
		outputs, err := PT(receiver).Create(ctx, configuration)
		if err != nil {
			return zero, err
		}
		return prepareResourceProviderResult(outputs)
	}
}

func newResourceProviderUpdate[
	In, Out, Config any,
	PT resourceUpdaterPtr[In, Out, Config],
](construct func() *In) resourceProviderUpdateFunc[In, Out, Config] {
	return func(
		ctx context.Context,
		inputs In,
		configuration Config,
		prior Prior[In, Out],
	) (preparedResourceApplyResult[Out], error) {
		var zero preparedResourceApplyResult[Out]
		receiver, err := prepareResourceReceiver(construct, inputs)
		if err != nil {
			return zero, err
		}
		outputs, err := PT(receiver).Update(ctx, configuration, prior)
		if err != nil {
			return zero, err
		}
		return prepareResourceProviderResult(outputs)
	}
}

func newResourceProviderDelete[
	In, Out, Config any,
	PT resourceDeleterPtr[In, Out, Config],
](construct func() *In) resourceProviderDeleteFunc[In, Out, Config] {
	return func(
		ctx context.Context,
		inputs In,
		configuration Config,
		encodedOutputs EncodedValue,
	) error {
		outputs, err := decodeResourceOutputs[Out](encodedOutputs)
		if err != nil {
			return err
		}
		receiver, err := prepareResourceReceiver(construct, inputs)
		if err != nil {
			return err
		}
		return PT(receiver).Delete(ctx, configuration, outputs)
	}
}

func prepareResourceProviderResult[Out any](
	outputs Out,
) (preparedResourceApplyResult[Out], error) {
	encoded, err := encodeResourceOutputs(outputs)
	if err != nil {
		return preparedResourceApplyResult[Out]{}, err
	}
	return preparedResourceApplyResult[Out]{
		Outputs: outputs,
		Encoded: encoded,
	}, nil
}
