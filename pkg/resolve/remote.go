package resolve

import (
	"errors"
	"fmt"
)

// ErrRemoteNotImplemented is returned by RemoteResolver.Resolve.
var ErrRemoteNotImplemented = errors.New("remote resolver not implemented")

// RemoteResolver resolves *RemoteImport refs by fetching the named git
// repo at the requested constraint, verifying the commit and content
// hash, and exposing the requested subdir as a Source.
type RemoteResolver struct{}

// NewRemoteResolver returns a RemoteResolver.
func NewRemoteResolver() *RemoteResolver {
	return &RemoteResolver{}
}

// Resolve implements Resolver. Local refs return a mismatch error;
// remote refs return ErrRemoteNotImplemented.
func (r *RemoteResolver) Resolve(ref ImportRef) (*Source, error) {
	if _, ok := ref.(*RemoteImport); !ok {
		return nil, fmt.Errorf("remote resolver cannot handle %T", ref)
	}
	return nil, ErrRemoteNotImplemented
}
