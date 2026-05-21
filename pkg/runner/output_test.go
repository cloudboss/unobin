package runner

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseFormat(t *testing.T) {
	cases := []struct {
		in      string
		want    Format
		wantErr bool
	}{
		{in: "", want: FormatText},
		{in: "text", want: FormatText},
		{in: "json", want: FormatJSON},
		{in: "unobin", want: FormatUnobin},
		{in: "yaml", wantErr: true},
		{in: "JSON", wantErr: true},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got, err := ParseFormat(c.in)
			if c.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, c.want, got)
		})
	}
}

func TestApplyEventEnvelopeJSON(t *testing.T) {
	ev := runtime.ApplyEvent{
		Kind:     runtime.NodeResource,
		Address:  "resource.aws.vpc.main",
		Decision: runtime.DecisionCreate,
		Stage:    runtime.StageDone,
		Time:     time.Date(2026, 5, 20, 15, 4, 5, 0, time.UTC),
		Elapsed:  1200 * time.Millisecond,
	}
	var buf bytes.Buffer
	require.NoError(t, writeEnvelope(&buf, FormatJSON, applyEventFrom(ev)))
	assert.Equal(t,
		`{"kind":"apply-event","time":"15:04:05","stage":"done","decision":"create","address":"resource.aws.vpc.main","elapsed":"1.2s"}`+"\n",
		buf.String())
}

func TestApplyEventEnvelopeUnobin(t *testing.T) {
	ev := runtime.ApplyEvent{
		Kind:     runtime.NodeResource,
		Address:  "resource.aws.vpc.main",
		Decision: runtime.DecisionCreate,
		Stage:    runtime.StageStart,
		Time:     time.Date(2026, 5, 20, 15, 4, 5, 0, time.UTC),
	}
	var buf bytes.Buffer
	require.NoError(t, writeEnvelope(&buf, FormatUnobin, applyEventFrom(ev)))
	assert.Equal(t,
		"{ kind: 'apply-event', time: '15:04:05', stage: 'start', decision: 'create',"+
			" address: 'resource.aws.vpc.main' }\n",
		buf.String())
}

func TestApplyEventFailCarriesError(t *testing.T) {
	ev := runtime.ApplyEvent{
		Kind:     runtime.NodeResource,
		Address:  "resource.aws.vpc.main",
		Decision: runtime.DecisionCreate,
		Stage:    runtime.StageFail,
		Time:     time.Date(2026, 5, 20, 15, 4, 5, 0, time.UTC),
		Elapsed:  400 * time.Millisecond,
		Err:      errors.New("vpc-abc123 not found"),
	}
	var buf bytes.Buffer
	require.NoError(t, writeEnvelope(&buf, FormatJSON, applyEventFrom(ev)))
	assert.Contains(t, buf.String(), `"stage":"fail"`)
	assert.Contains(t, buf.String(), `"err":"vpc-abc123 not found"`)
	assert.Contains(t, buf.String(), `"elapsed":"400ms"`)
}

func TestApplyOutputEnvelopes(t *testing.T) {
	outputs := map[string]any{
		"vpc-id": "vpc-abc123",
		"sizes":  []any{int64(1), int64(2)},
	}
	var buf bytes.Buffer
	require.NoError(t, writeApplyOutputs(&buf, FormatUnobin, outputs, nil))
	assert.Equal(t,
		"{ kind: 'apply-output', name: 'sizes', value: [1, 2] }\n"+
			"{ kind: 'apply-output', name: 'vpc-id', value: 'vpc-abc123' }\n",
		buf.String())
}

func TestApplyOutputEnvelopesJSON(t *testing.T) {
	outputs := map[string]any{
		"vpc-id": "vpc-abc123",
	}
	var buf bytes.Buffer
	require.NoError(t, writeApplyOutputs(&buf, FormatJSON, outputs, nil))
	assert.Equal(t,
		`{"kind":"apply-output","name":"vpc-id","value":"vpc-abc123"}`+"\n",
		buf.String())
}

func TestApplyOutputsText(t *testing.T) {
	outputs := map[string]any{"vpc-id": "vpc-abc123"}
	var buf bytes.Buffer
	require.NoError(t, writeApplyOutputs(&buf, FormatText, outputs, nil))
	assert.Equal(t, "vpc-id: 'vpc-abc123'\n", buf.String())
}

func TestApplyOutputEnvelopesMasksSensitive(t *testing.T) {
	outputs := map[string]any{
		"password": "shh",
		"vpc-id":   "vpc-abc123",
	}
	sensitive := map[string]bool{"password": true}
	var buf bytes.Buffer
	require.NoError(t, writeApplyOutputs(&buf, FormatUnobin, outputs, sensitive))
	assert.Equal(t,
		"{ kind: 'apply-output', name: 'password', value: '***' }\n"+
			"{ kind: 'apply-output', name: 'vpc-id', value: 'vpc-abc123' }\n",
		buf.String())
}

func TestApplyOutputsTextMasksSensitive(t *testing.T) {
	outputs := map[string]any{"password": "shh"}
	sensitive := map[string]bool{"password": true}
	var buf bytes.Buffer
	require.NoError(t, writeApplyOutputs(&buf, FormatText, outputs, sensitive))
	assert.Equal(t, "password: ***\n", buf.String())
}

func TestApplyErrorEnvelope(t *testing.T) {
	ae := &runtime.ApplyError{
		Address:        "resource.aws.vpc.main",
		Decision:       runtime.DecisionCreate,
		Module:         "aws",
		Elapsed:        400 * time.Millisecond,
		Err:            errors.New("InvalidVpcID.NotFound"),
		SkippedCount:   3,
		SucceededCount: 12,
	}
	var buf bytes.Buffer
	renderApplyError(&buf, ae, FormatJSON)
	out := buf.String()
	assert.Contains(t, out, `"kind":"apply-error"`)
	assert.Contains(t, out, `"address":"resource.aws.vpc.main"`)
	assert.Contains(t, out, `"err":"InvalidVpcID.NotFound"`)
	assert.Contains(t, out, `"skipped":3`)
	assert.Contains(t, out, `"succeeded":12`)
}
