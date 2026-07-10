package root

import (
	"io"

	"github.com/cloudboss/unobin/pkg/diagnostic"
)

type textDiagnosticReporter struct {
	out io.Writer
}

func (r textDiagnosticReporter) Report(d diagnostic.Diagnostic) {
	_ = diagnostic.WriteText(r.out, d)
}
