package program

import (
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/typecheck"
)

type CheckedBody struct {
	Imports    *Imports
	DAG        *runtime.DAG
	LocalTypes map[string]typecheck.Type
}
