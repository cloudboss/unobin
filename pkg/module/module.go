package module

import (
	"github.com/cloudboss/go-player/pkg/types"
)

type Module interface {
	Initialize(frame *types.Frame) error
	Name() string
	Build() *types.Result
	Destroy() *types.Result
}
