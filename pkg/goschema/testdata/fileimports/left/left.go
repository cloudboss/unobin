package left

import "context"

type Query struct {
	Name string
}

type Result struct {
	Name string
}

type Record struct {
	Value string `ub:"label"`
}

func (q *Query) Read(context.Context, any) (*Result, error) {
	return &Result{}, nil
}
