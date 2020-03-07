package lazy

import (
	"github.com/cloudboss/go-player/pkg/types"
)

type String func(*types.Frame) string
type Bool func(*types.Frame) bool

func S(s string) String {
	return func(*types.Frame) string {
		return s
	}
}

func B(b bool) Bool {
	return func(*types.Frame) bool {
		return b
	}
}

func False(*types.Frame) bool {
	return false
}

func True(*types.Frame) bool {
	return true
}

func EmptyString(*types.Frame) string {
	return ""
}
