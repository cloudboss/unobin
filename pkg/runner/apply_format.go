package runner

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"time"

	"github.com/cloudboss/unobin/internal/cmdout"
	"github.com/cloudboss/unobin/pkg/diagnostic"
	"github.com/cloudboss/unobin/pkg/runtime"
)

type applyStream struct {
	out      io.Writer
	format   cmdout.Format
	next     int64
	started  time.Time
	now      func() time.Time
	terminal bool
	outputs  bool
	ui       bool
	stateRev *string
}

type applyStreamEncodingError struct {
	Cause error
}

func (e *applyStreamEncodingError) Error() string {
	return e.Cause.Error()
}

func (e *applyStreamEncodingError) Unwrap() error {
	return e.Cause
}

type applyStreamWriteError struct {
	Cause error
}

func (e *applyStreamWriteError) Error() string {
	return e.Cause.Error()
}

func (e *applyStreamWriteError) Unwrap() error {
	return e.Cause
}

type applyDiagnosticRecord struct {
	Kind          string                `json:"kind"           ub:"kind"`
	FormatVersion int                   `json:"format-version" ub:"format-version"`
	Sequence      int64                 `json:"sequence"       ub:"sequence"`
	Timestamp     string                `json:"timestamp"      ub:"timestamp"`
	Diagnostic    diagnostic.Diagnostic `json:"diagnostic"     ub:"diagnostic"`
}

type applyUIRecord struct {
	Kind          string `json:"kind"           ub:"kind"`
	FormatVersion int    `json:"format-version" ub:"format-version"`
	Sequence      int64  `json:"sequence"       ub:"sequence"`
	Timestamp     string `json:"timestamp"      ub:"timestamp"`
	URL           string `json:"url"            ub:"url"`
}

type applyEventRecord struct {
	Kind          string `json:"kind"              ub:"kind"`
	FormatVersion int    `json:"format-version"    ub:"format-version"`
	Sequence      int64  `json:"sequence"          ub:"sequence"`
	Timestamp     string `json:"timestamp"         ub:"timestamp"`
	Stage         string `json:"stage"             ub:"stage"`
	Decision      string `json:"decision"          ub:"decision"`
	Address       string `json:"address"           ub:"address"`
	Elapsed       string `json:"elapsed,omitempty" ub:"elapsed,omitempty"`
}

type applyOutputRecord struct {
	Kind          string `json:"kind"           ub:"kind"`
	FormatVersion int    `json:"format-version" ub:"format-version"`
	Sequence      int64  `json:"sequence"       ub:"sequence"`
	Timestamp     string `json:"timestamp"      ub:"timestamp"`
	Name          string `json:"name"           ub:"name"`
	Value         any    `json:"value"          ub:"value"`
	Sensitive     bool   `json:"sensitive"      ub:"sensitive"`
}

type applyResultRecord struct {
	Kind          string `json:"kind"           ub:"kind"`
	FormatVersion int    `json:"format-version" ub:"format-version"`
	Sequence      int64  `json:"sequence"       ub:"sequence"`
	Timestamp     string `json:"timestamp"      ub:"timestamp"`
	StartedAt     string `json:"started-at"     ub:"started-at"`
	FinishedAt    string `json:"finished-at"    ub:"finished-at"`
	Elapsed       string `json:"elapsed"        ub:"elapsed"`
	StateRev      string `json:"state-rev"      ub:"state-rev"`
	OutputCount   int    `json:"output-count"   ub:"output-count"`
}

type applyErrorRecord struct {
	Kind          string                  `json:"kind"                ub:"kind"`
	FormatVersion int                     `json:"format-version"      ub:"format-version"`
	Sequence      int64                   `json:"sequence"            ub:"sequence"`
	Timestamp     string                  `json:"timestamp"           ub:"timestamp"`
	StartedAt     string                  `json:"started-at"          ub:"started-at"`
	FinishedAt    string                  `json:"finished-at"         ub:"finished-at"`
	Elapsed       string                  `json:"elapsed"             ub:"elapsed"`
	Stage         string                  `json:"stage"               ub:"stage"`
	Code          string                  `json:"code"                ub:"code"`
	Message       string                  `json:"message"             ub:"message"`
	StateRev      *string                 `json:"state-rev"           ub:"state-rev"`
	Diagnostics   []diagnostic.Diagnostic `json:"diagnostics"         ub:"diagnostics"`
	Address       string                  `json:"address,omitempty"   ub:"address,omitempty"`
	Decision      string                  `json:"decision,omitempty"  ub:"decision,omitempty"`
	Library       string                  `json:"library,omitempty"   ub:"library,omitempty"`
	Skipped       *int                    `json:"skipped,omitempty"   ub:"skipped,omitempty"`
	Succeeded     *int                    `json:"succeeded,omitempty" ub:"succeeded,omitempty"`
}

type preparedApplyOutput struct {
	Name      string
	Value     any
	Sensitive bool
}

func newApplyStream(
	out io.Writer,
	format cmdout.Format,
	now func() time.Time,
) *applyStream {
	if now == nil {
		now = time.Now
	}
	return &applyStream{
		out: out, format: format, next: 1, started: now().UTC(), now: now,
	}
}

func (s *applyStream) Diagnostic(value diagnostic.Diagnostic) error {
	if err := s.nonterminalAllowed("diagnostic"); err != nil {
		return err
	}
	return s.write(func(sequence int64, timestamp string, _ time.Time) any {
		return applyDiagnosticRecord{
			Kind: "command-diagnostic", FormatVersion: 1,
			Sequence: sequence, Timestamp: timestamp, Diagnostic: value,
		}
	})
}

func (s *applyStream) UI(url string) error {
	if err := s.nonterminalAllowed("UI"); err != nil {
		return err
	}
	if s.ui {
		return applyEncodingError(errors.New("apply stream: UI record already emitted"))
	}
	err := s.write(func(sequence int64, timestamp string, _ time.Time) any {
		return applyUIRecord{
			Kind: "apply-ui", FormatVersion: 1,
			Sequence: sequence, Timestamp: timestamp, URL: url,
		}
	})
	if err == nil {
		s.ui = true
	}
	return err
}

func (s *applyStream) Event(event runtime.ApplyEvent) error {
	if event.Stage == runtime.StageFail {
		return nil
	}
	if err := s.nonterminalAllowed("event"); err != nil {
		return err
	}
	stage := string(event.Stage)
	elapsed := ""
	switch event.Stage {
	case runtime.StageStart:
	case runtime.StageDone:
		elapsed = formatDuration(event.Elapsed)
	default:
		return applyEncodingError(fmt.Errorf(
			"apply stream: unsupported event stage %q", event.Stage,
		))
	}
	return s.write(func(sequence int64, timestamp string, _ time.Time) any {
		return applyEventRecord{
			Kind: "apply-event", FormatVersion: 1,
			Sequence: sequence, Timestamp: timestamp, Stage: stage,
			Decision: string(event.Decision), Address: event.Address, Elapsed: elapsed,
		}
	})
}

func (s *applyStream) Output(name string, value any, sensitive bool) error {
	if s.terminal {
		return applyEncodingError(errors.New("apply stream: output after terminal record"))
	}
	if sensitive {
		value = sensitivePlaceholder
	}
	err := s.write(func(sequence int64, timestamp string, _ time.Time) any {
		return applyOutputRecord{
			Kind: "apply-output", FormatVersion: 1,
			Sequence: sequence, Timestamp: timestamp,
			Name: name, Value: value, Sensitive: sensitive,
		}
	})
	if err == nil {
		s.outputs = true
	}
	return err
}

func (s *applyStream) Result(result *runtime.ExecResult) error {
	if err := s.terminalAllowed(); err != nil {
		return err
	}
	if err := validateApplyResult(result); err != nil {
		return applyEncodingError(err)
	}
	err := s.write(func(sequence int64, timestamp string, finished time.Time) any {
		return applyResultRecord{
			Kind: "apply-result", FormatVersion: 1,
			Sequence: sequence, Timestamp: timestamp,
			StartedAt: formatApplyTimestamp(s.started), FinishedAt: timestamp,
			Elapsed:  formatDuration(finished.Sub(s.started)),
			StateRev: result.WrittenRev, OutputCount: len(result.Outputs),
		}
	})
	if err == nil {
		s.terminal = true
	}
	return err
}

func validateApplyResult(result *runtime.ExecResult) error {
	if result == nil {
		return errors.New("apply stream: result is required")
	}
	if result.WrittenRev == "" {
		return errors.New("apply stream: result state revision is required")
	}
	return nil
}

func (s *applyStream) Error(failure *runtime.ApplyFailure) error {
	if err := s.terminalAllowed(); err != nil {
		return err
	}
	if s.outputs {
		return applyEncodingError(errors.New("apply stream: error after output record"))
	}
	record, err := s.applyErrorRecord(failure)
	if err != nil {
		return applyEncodingError(err)
	}
	err = s.write(func(sequence int64, timestamp string, finished time.Time) any {
		record.Sequence = sequence
		record.Timestamp = timestamp
		record.StartedAt = formatApplyTimestamp(s.started)
		record.FinishedAt = timestamp
		record.Elapsed = formatDuration(finished.Sub(s.started))
		return record
	})
	if err == nil {
		s.terminal = true
	}
	return err
}

func (s *applyStream) nonterminalAllowed(record string) error {
	if s.terminal {
		return applyEncodingError(fmt.Errorf(
			"apply stream: %s after terminal record", record,
		))
	}
	if s.outputs {
		return applyEncodingError(fmt.Errorf(
			"apply stream: %s after output record", record,
		))
	}
	return nil
}

func (s *applyStream) terminalAllowed() error {
	if s.terminal {
		return applyEncodingError(errors.New("apply stream: terminal record already emitted"))
	}
	return nil
}

func (s *applyStream) write(
	build func(sequence int64, timestamp string, at time.Time) any,
) error {
	at := s.now().UTC()
	value := build(s.next, formatApplyTimestamp(at), at)
	encoded, err := cmdout.EncodeRecord(s.format, value)
	if err != nil {
		return applyEncodingError(err)
	}
	if s.out == nil {
		return &applyStreamWriteError{Cause: errors.New("apply stream: output writer is required")}
	}
	written, err := s.out.Write(encoded)
	if err != nil {
		return &applyStreamWriteError{Cause: err}
	}
	if written != len(encoded) {
		return &applyStreamWriteError{Cause: io.ErrShortWrite}
	}
	s.next++
	return nil
}

func (s *applyStream) applyErrorRecord(
	failure *runtime.ApplyFailure,
) (applyErrorRecord, error) {
	if failure == nil || failure.Cause == nil {
		return applyErrorRecord{}, errors.New("apply stream: failure is required")
	}
	switch failure.Stage {
	case runtime.ApplyFailureSetup,
		runtime.ApplyFailureExecute,
		runtime.ApplyFailureFinalize:
	default:
		return applyErrorRecord{}, fmt.Errorf(
			"apply stream: unsupported failure stage %q", failure.Stage,
		)
	}
	record := applyErrorRecord{
		Kind: "apply-error", FormatVersion: 1, Stage: string(failure.Stage),
		StateRev: copyOptionalString(s.stateRev),
	}
	interrupted := errors.Is(failure.Cause, runtime.ErrInterrupted)
	var step *runtime.ApplyError
	hasStep := errors.As(failure.Cause, &step)
	switch {
	case interrupted:
		record.Code = "unobin.apply.interrupted"
		record.Message = "apply interrupted"
	case hasStep:
		record.Code = "unobin.apply.step-failed"
		record.Message = fmt.Sprintf("%s failed for %s", step.Decision, step.Address)
	case failure.Stage == runtime.ApplyFailureSetup:
		record.Code = "unobin.apply.setup-failed"
		record.Message = "apply setup failed"
	case failure.Stage == runtime.ApplyFailureFinalize:
		record.Code = "unobin.apply.finalize-failed"
		record.Message = "apply finalization failed"
	default:
		return applyErrorRecord{}, fmt.Errorf(
			"apply stream: execute failure has no step or interruption",
		)
	}
	record.Diagnostics = applyFailureDiagnostics(failure.Cause, interrupted)
	if hasStep {
		record.Address = step.Address
		record.Decision = string(step.Decision)
		record.Library = step.LibraryPath
		skipped := step.SkippedCount
		succeeded := step.SucceededCount
		record.Skipped = &skipped
		record.Succeeded = &succeeded
	}
	return record, nil
}

func applyFailureDiagnostics(cause error, interrupted bool) []diagnostic.Diagnostic {
	diagnostics := stateErrorDiagnostics(cause)
	if !interrupted {
		return diagnostics
	}
	out := make([]diagnostic.Diagnostic, 0, len(diagnostics))
	for _, value := range diagnostics {
		if value.Message == runtime.ErrInterrupted.Error() {
			continue
		}
		out = append(out, value)
	}
	return diagnostic.Normalize(out)
}

func prepareApplyOutputs(
	format cmdout.Format,
	outputs map[string]any,
	sensitive map[string]bool,
) ([]preparedApplyOutput, error) {
	names := make([]string, 0, len(outputs))
	for name := range outputs {
		names = append(names, name)
	}
	slices.Sort(names)
	prepared := make([]preparedApplyOutput, 0, len(names))
	for _, name := range names {
		value := outputs[name]
		isSensitive := sensitive[name]
		if isSensitive {
			value = sensitivePlaceholder
		}
		record := applyOutputRecord{
			Kind: "apply-output", FormatVersion: 1, Sequence: 1,
			Timestamp: "2000-01-01T00:00:00Z", Name: name,
			Value: value, Sensitive: isSensitive,
		}
		if _, err := cmdout.EncodeRecord(format, record); err != nil {
			return nil, applyEncodingError(err)
		}
		prepared = append(prepared, preparedApplyOutput{
			Name: name, Value: value, Sensitive: isSensitive,
		})
	}
	return prepared, nil
}

func formatApplyTimestamp(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func applyEncodingError(err error) error {
	if err == nil {
		return nil
	}
	return &applyStreamEncodingError{Cause: err}
}
