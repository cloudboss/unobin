package samepkg

import (
	"strings"

	"github.com/cloudboss/unobin/pkg/runtime"
)

func Library() *runtime.Library {
	return &runtime.Library{
		Name: "samepkg",
		Actions: map[string]runtime.ActionRegistration{
			"do":  runtime.MakeAction[DoAction, *DoActionOutput, any](),
			"do2": runtime.MakeAction[Do2Action, *Do2ActionOutput, any](),
		},
		Functions: map[string]runtime.FunctionType{
			"upper": runtime.MakeFunc("upper", "Uppercase a string.", fnUpper),
			"pair":  runtime.MakeFunc("pair", "Pair two strings.", fnPair),
			"join":  runtime.MakeFunc("join", "Join strings.", fnJoin),
			"size":  runtime.MakeFunc("size", "Measure byte content.", fnSize),
		},
	}
}

func fnUpper(s string) (string, error) { return strings.ToUpper(s), nil }

func fnPair(a, b string) (string, error) { return a + b, nil }

func fnJoin(sep string, parts ...string) (string, error) {
	return strings.Join(parts, sep), nil
}

func fnSize(content []byte) (int64, error) {
	return int64(len(content)), nil
}
