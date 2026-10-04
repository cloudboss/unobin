package sourcecheck

import (
	"errors"

	"github.com/cloudboss/unobin/pkg/resolve"
)

type uncachedModuleError struct{}

func (*uncachedModuleError) Error() string {
	return "selected module source is not cached"
}

type cachedModuleResolver struct {
	wrapped resolve.Resolver
}

func (r cachedModuleResolver) Resolve(ref resolve.ImportRef) (*resolve.Source, error) {
	source, err := r.wrapped.Resolve(ref)
	if err != nil {
		return nil, err
	}
	if source == nil || source.Path == "" {
		return nil, &uncachedModuleError{}
	}
	return source, nil
}

func deferUncachedMetadata(err error) (bool, error) {
	switch failure := err.(type) {
	case *uncachedModuleError:
		return true, nil
	case interface{ Unwrap() []error }:
		var deferred bool
		var remaining []error
		for _, child := range failure.Unwrap() {
			missing, retained := deferUncachedMetadata(child)
			deferred = deferred || missing
			if retained != nil {
				remaining = append(remaining, retained)
			}
		}
		if deferred {
			return true, errors.Join(remaining...)
		}
	case interface{ Unwrap() error }:
		deferred, remaining := deferUncachedMetadata(failure.Unwrap())
		if deferred {
			return true, remaining
		}
	}
	return false, err
}
