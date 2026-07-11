package runner

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/cmdout"
	"github.com/cloudboss/unobin/pkg/diagnostic"
	"github.com/cloudboss/unobin/pkg/runtime"
)

func TestApplyStreamFormatGolden(t *testing.T) {
	for _, tc := range []struct {
		format cmdout.Format
		path   string
	}{
		{format: cmdout.FormatJSON, path: "testdata/apply-stream.jsonl"},
		{format: cmdout.FormatUnobin, path: "testdata/apply-stream-unobin.stdout"},
	} {
		var got bytes.Buffer
		writeApplySuccessStream(t, &got, tc.format)
		writeApplyErrorStreams(t, &got, tc.format)
		want, err := os.ReadFile(tc.path)
		require.NoError(t, err)
		require.Equal(t, string(want), got.String())
	}
}

func writeApplySuccessStream(t *testing.T, out io.Writer, format cmdout.Format) {
	t.Helper()
	stream := newApplyStream(out, format, applyTestClock())
	require.NoError(t, stream.Diagnostic(diagnostic.Diagnostic{
		Code: "unobin.command.deprecated-option", Severity: diagnostic.SeverityWarning,
		Message: "--output is deprecated; use --format instead",
	}))
	require.NoError(t, stream.UI("http://127.0.0.1:1234/token/"))
	require.NoError(t, stream.Event(runtime.ApplyEvent{
		Stage: runtime.StageStart, Decision: runtime.DecisionCreate,
		Address: "resource.greeting",
	}))
	require.NoError(t, stream.Event(runtime.ApplyEvent{
		Stage: runtime.StageFail, Decision: runtime.DecisionCreate,
		Address: "resource.hidden", Err: errors.New("hidden failure"),
	}))
	require.NoError(t, stream.Event(runtime.ApplyEvent{
		Stage: runtime.StageDone, Decision: runtime.DecisionCreate,
		Address: "resource.greeting", Elapsed: 1200 * time.Millisecond,
	}))
	require.NoError(t, stream.Output("password", func() {}, true))
	require.NoError(t, stream.Output("path", "/tmp/example", false))
	require.NoError(t, stream.Result(&runtime.ExecResult{
		WrittenRev: "revision-1",
		Outputs:    map[string]any{"password": "secret", "path": "/tmp/example"},
	}))
}

func writeApplyErrorStreams(t *testing.T, out io.Writer, format cmdout.Format) {
	t.Helper()
	setup := newApplyStream(out, format, applyTestClock())
	require.NoError(t, setup.Error(runtime.NewApplyFailure(
		runtime.ApplyFailureSetup, errors.New("open plan failed"),
	)))

	stepError := &runtime.ApplyError{
		Address: "resource.greeting", Decision: runtime.DecisionCreate,
		LibraryPath: "example.com/local", Err: errors.New("permission denied"),
		SkippedCount: 0, SucceededCount: 2,
	}
	execute := newApplyStream(out, format, applyTestClock())
	revision := "revision-2"
	execute.stateRev = &revision
	require.NoError(t, execute.Error(runtime.NewApplyFailure(
		runtime.ApplyFailureExecute, stepError,
	)))

	interrupted := newApplyStream(out, format, applyTestClock())
	interrupted.stateRev = &revision
	require.NoError(t, interrupted.Error(runtime.NewApplyFailure(
		runtime.ApplyFailureExecute, errors.Join(runtime.ErrInterrupted, stepError),
	)))

	finalize := newApplyStream(out, format, applyTestClock())
	finalize.stateRev = &revision
	require.NoError(t, finalize.Error(runtime.NewApplyFailure(
		runtime.ApplyFailureFinalize,
		errors.Join(
			errors.New("persist failed"),
			&runtime.StateUnlockError{Cause: errors.New("unlock failed")},
		),
	)))
}

type applyStreamStateGolden struct {
	Cases []applyStreamStateCaseGolden `json:"cases"`
}

type applyStreamStateCaseGolden struct {
	Name     string `json:"name"`
	Error    string `json:"error"`
	Encoding bool   `json:"encoding"`
	Writing  bool   `json:"writing"`
	Next     int64  `json:"next"`
	UI       bool   `json:"ui"`
	Outputs  bool   `json:"outputs"`
	Terminal bool   `json:"terminal"`
	Written  int    `json:"written-bytes"`
	Prepared string `json:"prepared"`
}

func TestApplyStreamStateGolden(t *testing.T) {
	result := applyStreamStateGolden{}
	streamBuffer := &bytes.Buffer{}
	stream := newApplyStream(streamBuffer, cmdout.FormatJSON, applyTestClock())
	require.NoError(t, stream.UI("http://127.0.0.1/run"))
	result.Cases = append(result.Cases,
		applyStreamStateCase("duplicate UI", stream, streamBuffer, stream.UI("second")))
	result.Cases = append(result.Cases, applyStreamStateCase(
		"invalid public output", stream, streamBuffer,
		stream.Output("invalid", func() {}, false),
	))
	require.NoError(t, stream.Output("secret", func() {}, true))
	result.Cases = append(result.Cases, applyStreamStateCase(
		"diagnostic after output", stream, streamBuffer,
		stream.Diagnostic(diagnostic.Diagnostic{Code: "late"}),
	))
	result.Cases = append(result.Cases, applyStreamStateCase(
		"event after output", stream, streamBuffer,
		stream.Event(runtime.ApplyEvent{Stage: runtime.StageStart}),
	))
	result.Cases = append(result.Cases, applyStreamStateCase(
		"error after output", stream, streamBuffer,
		stream.Error(runtime.NewApplyFailure(
			runtime.ApplyFailureSetup, errors.New("late failure"),
		)),
	))
	require.NoError(t, stream.Result(&runtime.ExecResult{
		WrittenRev: "revision", Outputs: map[string]any{"secret": "hidden"},
	}))
	result.Cases = append(result.Cases, applyStreamStateCase(
		"second terminal", stream, streamBuffer,
		stream.Result(&runtime.ExecResult{WrittenRev: "revision"}),
	))

	invalidStageBuffer := &bytes.Buffer{}
	invalidStage := newApplyStream(invalidStageBuffer, cmdout.FormatJSON, applyTestClock())
	result.Cases = append(result.Cases, applyStreamStateCase(
		"invalid event stage", invalidStage, invalidStageBuffer,
		invalidStage.Event(runtime.ApplyEvent{Stage: "queued"}),
	))
	result.Cases = append(result.Cases, applyStreamStateCase(
		"unclassified execute failure", invalidStage, invalidStageBuffer,
		invalidStage.Error(runtime.NewApplyFailure(
			runtime.ApplyFailureExecute, errors.New("scheduler failed"),
		)),
	))

	writeFailure := newApplyStream(applyBrokenWriter{}, cmdout.FormatJSON, applyTestClock())
	result.Cases = append(result.Cases, applyStreamStateCase(
		"write failure", writeFailure, nil,
		writeFailure.Diagnostic(diagnostic.Diagnostic{Code: "first"}),
	))
	shortWrite := newApplyStream(applyShortWriter{}, cmdout.FormatJSON, applyTestClock())
	result.Cases = append(result.Cases, applyStreamStateCase(
		"short write", shortWrite, nil,
		shortWrite.Diagnostic(diagnostic.Diagnostic{Code: "first"}),
	))

	masked, maskedErr := prepareApplyOutputs(
		cmdout.FormatJSON,
		map[string]any{"secret": func() {}},
		map[string]bool{"secret": true},
	)
	maskedStream := &applyStream{next: int64(len(masked))}
	maskedCase := applyStreamStateCase(
		"preflight masks sensitive output", maskedStream, nil, maskedErr,
	)
	if len(masked) == 1 {
		maskedCase.Prepared = fmt.Sprintf("%s=%v sensitive=%t",
			masked[0].Name, masked[0].Value, masked[0].Sensitive)
	}
	result.Cases = append(result.Cases, maskedCase)

	prepared, prepareErr := prepareApplyOutputs(
		cmdout.FormatJSON,
		map[string]any{"public": func() {}, "secret": func() {}},
		map[string]bool{"secret": true},
	)
	prepareStream := &applyStream{next: int64(len(prepared))}
	result.Cases = append(result.Cases, applyStreamStateCase(
		"preflight output failure", prepareStream, nil, prepareErr,
	))

	got, err := json.MarshalIndent(result, "", "  ")
	require.NoError(t, err)
	got = append(got, '\n')
	want, err := os.ReadFile("testdata/apply-stream-state.json")
	require.NoError(t, err)
	require.Equal(t, string(want), string(got))
}

func applyStreamStateCase(
	name string,
	stream *applyStream,
	buffer *bytes.Buffer,
	err error,
) applyStreamStateCaseGolden {
	var encoding *applyStreamEncodingError
	var writing *applyStreamWriteError
	result := applyStreamStateCaseGolden{
		Name: name, Encoding: errors.As(err, &encoding), Writing: errors.As(err, &writing),
		Next: stream.next, UI: stream.ui, Outputs: stream.outputs, Terminal: stream.terminal,
	}
	if err != nil {
		result.Error = err.Error()
	}
	if buffer != nil {
		result.Written = buffer.Len()
	}
	return result
}

type applyBrokenWriter struct{}

func (applyBrokenWriter) Write([]byte) (int, error) {
	return 0, errors.New("writer failed")
}

type applyShortWriter struct{}

func (applyShortWriter) Write(value []byte) (int, error) {
	return len(value) - 1, nil
}

func applyTestClock() func() time.Time {
	base := time.Date(2026, 7, 9, 14, 32, 18, 0, time.UTC)
	var tick int
	return func() time.Time {
		value := base.Add(time.Duration(tick) * 100 * time.Millisecond)
		tick++
		return value
	}
}
