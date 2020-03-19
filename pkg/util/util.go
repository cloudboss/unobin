package util

import (
	"github.com/cloudboss/go-player/pkg/types"
)

func FilterEmpty(items []string) []string {
	nonEmpty := []string{}
	for _, item := range items {
		if item != "" {
			nonEmpty = append(nonEmpty, item)
		}
	}
	return nonEmpty
}

func Any(bools []bool) bool {
	for _, b := range bools {
		if b {
			return b
		}
	}
	return false
}

func All(bools []bool) bool {
	for _, b := range bools {
		if !b {
			return false
		}
	}
	return true
}

func ErrResult(msg, moduleName string) *types.Result {
	return &types.Result{
		Succeeded: false,
		Changed:   false,
		Error:     msg,
		Module:    moduleName,
	}
}
