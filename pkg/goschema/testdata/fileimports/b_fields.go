package fileimports

import (
	"context"
	moment "time"

	fields "example.com/fileimports/left"
	"example.com/fileimports/odd/v2"
	types "example.com/fileimports/right"
)

type Local struct {
	Nested    types.Record
	Alias     fields.Record
	Weird     model.Record
	Timeout   moment.Duration
	Primary   *string
	Secondary *string
}

type LocalOutput = types.Result

func (v *Local) Read(context.Context, any) (*LocalOutput, error) {
	return &LocalOutput{}, nil
}
