package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/cloudboss/unobin/internal/cmdout"
	"github.com/cloudboss/unobin/pkg/diagnostic"
	"github.com/cloudboss/unobin/pkg/runtime"
	sdkencrypt "github.com/cloudboss/unobin/pkg/sdk/encrypt"
	"github.com/cloudboss/unobin/pkg/sdk/state"
	"github.com/cloudboss/unobin/pkg/ui"
	"github.com/spf13/cobra"
)

const applyBrowserTimeout = 5 * time.Second

type preparedApplyCommand struct {
	plan        *runtime.PlanFile
	parsed      *parsedFactory
	store       state.Backend
	parallelism int
}

type applyRunView interface {
	URL() string
	Observe(runtime.ApplyEvent)
	Complete(bool, string)
	WaitServed(time.Duration) bool
	Close()
}

type applyMachineRequest struct {
	diagnostic *diagnostic.Diagnostic
	event      *runtime.ApplyEvent
}

type applyRuntimeOutcome struct {
	result  *runtime.ExecResult
	failure *runtime.ApplyFailure
}

type applyMachineOptions struct {
	now            func() time.Time
	browserTimeout time.Duration
	openBrowser    func(context.Context, string) error
	startView      func(Info, *runtime.PlanFile, *runtime.DAG) (applyRunView, error)
	apply          func(context.Context, *runtime.Executor, *runtime.PlanFile) (
		*runtime.ExecResult, error,
	)
}

func runApplyCommand(
	command *cobra.Command,
	info Info,
	planPath string,
	parallelism int,
	outputValue string,
	withUI bool,
) error {
	format, deprecated, conflict, err := resolveApplyCommandFormat(command, outputValue)
	if err != nil {
		return err
	}
	controller := newSystemApplySignalController(context.Background())
	startup, startupErr := linkedUnobinDiagnostic(info.UnobinVersion)
	if format == cmdout.FormatText {
		return runApplyTextCommand(
			command, info, planPath, parallelism, withUI,
			deprecated, conflict, startup, startupErr, controller,
		)
	}
	return runApplyMachineCommand(
		command, info, planPath, parallelism, withUI,
		format, deprecated, conflict, startup, startupErr, controller,
		applyMachineOptions{},
	)
}

func resolveApplyCommandFormat(
	command *cobra.Command,
	outputValue string,
) (cmdout.Format, bool, error, error) {
	formatChanged := command.Flags().Changed("format")
	outputChanged := command.Flags().Changed("output")
	format := cmdout.FormatText
	if formatChanged {
		value, err := command.Flags().GetString("format")
		if err != nil {
			return "", false, nil, err
		}
		parsed, err := cmdout.ParseFormat(value)
		if err != nil {
			return "", false, nil, err
		}
		format = parsed
	}
	if outputChanged {
		parsed, err := ParseFormat(outputValue)
		if err != nil {
			return "", false, nil, err
		}
		if !formatChanged {
			format = cmdout.Format(parsed)
		}
	}
	if formatChanged && outputChanged {
		return format, false, errors.New("--format and --output cannot be used together"), nil
	}
	return format, outputChanged, nil, nil
}

func applyDeprecationDiagnostic() diagnostic.Diagnostic {
	return diagnostic.Diagnostic{
		Code: "unobin.command.deprecated-option", Severity: diagnostic.SeverityWarning,
		Message: "--output is deprecated; use --format instead",
	}
}

func prepareApplyCommand(
	info Info,
	planPath string,
	parallelismOverride int,
) (*preparedApplyCommand, *runtime.ApplyFailure) {
	sealed, err := os.ReadFile(planPath)
	if err != nil {
		return nil, runtime.NewApplyFailure(runtime.ApplyFailureSetup, err)
	}
	var encrypter sdkencrypt.Encrypter
	plan, err := runtime.OpenPlan(
		sealed,
		func(ref *runtime.StateRef) (sdkencrypt.Encrypter, error) {
			resolved, err := resolveEncrypter(fromRuntimeStateRef(ref))
			if err != nil {
				return nil, err
			}
			encrypter = resolved
			return resolved, nil
		},
	)
	if err != nil {
		return nil, runtime.NewApplyFailure(runtime.ApplyFailureSetup, err)
	}
	parsed, err := parseFactory(info)
	if err != nil {
		return nil, runtime.NewApplyFailure(runtime.ApplyFailureSetup, err)
	}
	store, err := resolveBackend(
		fromRuntimeStateRef(plan.Backend), info.FactoryName, plan.Stack, encrypter,
	)
	if err != nil {
		return nil, runtime.NewApplyFailure(runtime.ApplyFailureSetup, err)
	}
	parallelism := plan.Parallelism
	if parallelismOverride > 0 {
		parallelism = parallelismOverride
	}
	return &preparedApplyCommand{
		plan: plan, parsed: parsed, store: store, parallelism: parallelism,
	}, nil
}

func runApplyTextCommand(
	command *cobra.Command,
	info Info,
	planPath string,
	parallelism int,
	withUI bool,
	deprecated bool,
	conflict error,
	startup diagnostic.Diagnostic,
	startupErr error,
	controller *applySignalController,
) error {
	if deprecated {
		if err := diagnostic.WriteText(command.ErrOrStderr(), applyDeprecationDiagnostic()); err != nil {
			controller.Stop()
			return err
		}
	}
	if startupErr != nil {
		controller.Stop()
		return startupErr
	}
	if startup.Message != "" {
		if err := diagnostic.WriteText(command.ErrOrStderr(), startup); err != nil {
			controller.Stop()
			return err
		}
	}
	noticesDone := forwardApplyTextNotices(command, controller)
	defer func() {
		controller.Stop()
		<-noticesDone
	}()
	if conflict != nil {
		return conflict
	}
	prepared, failure := prepareApplyCommand(info, planPath, parallelism)
	if failure != nil {
		return failure
	}
	return executeApplyText(command, info, prepared, withUI, controller)
}

func forwardApplyTextNotices(
	command *cobra.Command,
	controller *applySignalController,
) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		for notice := range controller.Notices() {
			fmt.Fprintln(command.ErrOrStderr(), notice.Message)
		}
	}()
	return done
}

func executeApplyText(
	command *cobra.Command,
	info Info,
	prepared *preparedApplyCommand,
	withUI bool,
	controller *applySignalController,
) error {
	events := make(chan runtime.ApplyEvent, len(prepared.plan.Steps)*3+16)
	rendererEvents := events
	var view applyRunView
	var browser <-chan *diagnostic.Diagnostic
	if withUI {
		var err error
		view, err = startApplyRunView(info, prepared.plan, prepared.parsed.dag)
		if err != nil {
			return runtime.NewApplyFailure(runtime.ApplyFailureSetup, err)
		}
		defer func() {
			view.WaitServed(uiLingerTimeout)
			view.Close()
		}()
		fmt.Fprintf(command.ErrOrStderr(), "Run view: %s\n", view.URL())
		browser = startApplyBrowser(ui.OpenBrowserContext, view.URL())
		rendererEvents = make(chan runtime.ApplyEvent, cap(events))
		go teeApplyEvents(events, rendererEvents, view)
	}
	rendererDone := make(chan struct{})
	go func() {
		defer close(rendererDone)
		consumeApplyEvents(rendererEvents, command.ErrOrStderr(), FormatText)
	}()
	executor := newApplyExecutor(info, prepared, controller, events)
	result, err := executor.ApplyPlan(controller.Context(), prepared.plan)
	close(events)
	<-rendererDone
	if browser != nil {
		if notice := <-browser; notice != nil {
			fmt.Fprintln(command.ErrOrStderr(), notice.Message)
		}
	}
	if view != nil {
		view.Complete(err == nil, runViewMessage(err))
	}
	if err != nil {
		if applyError, ok := errors.AsType[*runtime.ApplyError](err); ok {
			renderApplyError(command.ErrOrStderr(), applyError, FormatText)
		}
		return err
	}
	return writeApplyOutputs(
		command.OutOrStdout(), FormatText, result.Outputs,
		rootSensitiveOutputs(prepared.parsed),
	)
}

func runApplyMachineCommand(
	command *cobra.Command,
	info Info,
	planPath string,
	parallelism int,
	withUI bool,
	format cmdout.Format,
	deprecated bool,
	conflict error,
	startup diagnostic.Diagnostic,
	startupErr error,
	controller *applySignalController,
	options applyMachineOptions,
) error {
	if options.now == nil {
		options.now = time.Now
	}
	if options.openBrowser == nil {
		options.openBrowser = ui.OpenBrowserContext
	}
	if options.browserTimeout <= 0 {
		options.browserTimeout = applyBrowserTimeout
	}
	if options.startView == nil {
		options.startView = startApplyRunView
	}
	if options.apply == nil {
		options.apply = func(
			ctx context.Context,
			executor *runtime.Executor,
			plan *runtime.PlanFile,
		) (*runtime.ExecResult, error) {
			return executor.ApplyPlan(ctx, plan)
		}
	}
	stream := newApplyStream(command.OutOrStdout(), format, options.now)
	if deprecated {
		if err := stream.Diagnostic(applyDeprecationDiagnostic()); err != nil {
			return finishApplyInitialStreamError(stream, controller, nil, err, nil)
		}
	}
	if startup.Message != "" {
		if err := stream.Diagnostic(startup); err != nil {
			return finishApplyInitialStreamError(stream, controller, nil, err, nil)
		}
	}
	if startupErr != nil {
		return finishApplyMachineSetup(
			stream, controller, nil,
			runtime.NewApplyFailure(runtime.ApplyFailureSetup, startupErr),
		)
	}
	if conflict != nil {
		return finishApplyMachineSetup(
			stream, controller, nil,
			runtime.NewApplyFailure(runtime.ApplyFailureSetup, conflict),
		)
	}
	prepared, failure := prepareApplyCommand(info, planPath, parallelism)
	if failure != nil {
		return finishApplyMachineSetup(stream, controller, nil, failure)
	}
	if err := emitAvailableApplyNotices(stream, controller.Notices()); err != nil {
		return finishApplyInitialStreamError(
			stream, controller, prepared.store, err, nil,
		)
	}
	var view applyRunView
	if withUI {
		var err error
		view, err = options.startView(info, prepared.plan, prepared.parsed.dag)
		if err != nil {
			return finishApplyMachineSetup(
				stream, controller, prepared.store,
				runtime.NewApplyFailure(runtime.ApplyFailureSetup, err),
			)
		}
		defer func() {
			view.WaitServed(uiLingerTimeout)
			view.Close()
		}()
		if err := stream.UI(view.URL()); err != nil {
			return finishApplyInitialStreamError(
				stream, controller, prepared.store, err, nil,
			)
		}
	}
	return coordinateApplyMachine(
		stream, info, prepared, controller, view, options,
	)
}

func finishApplyMachineSetup(
	stream *applyStream,
	controller *applySignalController,
	store state.Backend,
	failure *runtime.ApplyFailure,
) error {
	controller.Stop()
	var streamErr error
	for notice := range controller.Notices() {
		if streamErr != nil {
			continue
		}
		streamErr = stream.Diagnostic(notice)
	}
	if streamErr != nil {
		return finishApplyEncodingOrWriteError(stream, store, streamErr, failure)
	}
	failure = enrichApplyFailure(failure, controller.SignalCause(), store, stream)
	return finishApplyMachineFailure(stream, store, failure)
}

func finishApplyInitialStreamError(
	stream *applyStream,
	controller *applySignalController,
	store state.Backend,
	streamErr error,
	ordinary error,
) error {
	controller.Cancel(streamErr)
	controller.Stop()
	return finishApplyEncodingOrWriteError(stream, store, streamErr, ordinary)
}

func emitAvailableApplyNotices(
	stream *applyStream,
	notices <-chan diagnostic.Diagnostic,
) error {
	for {
		select {
		case notice, ok := <-notices:
			if !ok {
				return nil
			}
			if err := stream.Diagnostic(notice); err != nil {
				return err
			}
		default:
			return nil
		}
	}
}

func coordinateApplyMachine(
	stream *applyStream,
	info Info,
	prepared *preparedApplyCommand,
	controller *applySignalController,
	view applyRunView,
	options applyMachineOptions,
) (returnErr error) {
	defer func() {
		if view != nil {
			view.Complete(returnErr == nil, runViewMessage(returnErr))
		}
	}()
	requests := make(chan applyMachineRequest, len(prepared.plan.Steps)*3+16)
	outcome := make(chan applyRuntimeOutcome, 1)
	events := make(chan runtime.ApplyEvent, len(prepared.plan.Steps)*3+16)
	var producers sync.WaitGroup

	producers.Go(func() {
		for notice := range controller.Notices() {
			value := notice
			requests <- applyMachineRequest{diagnostic: &value}
		}
	})

	producers.Go(func() {
		for event := range events {
			if view != nil {
				view.Observe(event)
			}
			if event.Stage == runtime.StageFail || isSilentEvent(event) {
				continue
			}
			value := event
			requests <- applyMachineRequest{event: &value}
		}
	})

	browserCancel := func() {}
	if view != nil {
		browserCtx, cancel := context.WithTimeout(
			context.Background(), options.browserTimeout,
		)
		browserCancel = cancel
		producers.Go(func() {
			defer cancel()
			if notice := applyBrowserNotice(
				options.openBrowser(browserCtx, view.URL()),
			); notice != nil {
				requests <- applyMachineRequest{diagnostic: notice}
			}
		})
	}
	defer browserCancel()

	producers.Go(func() {
		executor := newApplyExecutor(info, prepared, controller, events)
		result, err := options.apply(controller.Context(), executor, prepared.plan)
		close(events)
		controller.Stop()
		failure, ok := runtime.AsApplyFailure(err)
		if err != nil && !ok {
			failure = runtime.NewApplyFailure(runtime.ApplyFailureExecute, err)
		}
		outcome <- applyRuntimeOutcome{result: result, failure: failure}
	})

	go func() {
		producers.Wait()
		close(requests)
	}()

	var streamErr error
	for request := range requests {
		if streamErr != nil {
			continue
		}
		switch {
		case request.diagnostic != nil:
			streamErr = stream.Diagnostic(*request.diagnostic)
		case request.event != nil:
			streamErr = stream.Event(*request.event)
		}
		if streamErr != nil {
			controller.Cancel(streamErr)
			browserCancel()
		}
	}
	runtimeOutcome := <-outcome
	failure := enrichApplyFailure(
		runtimeOutcome.failure, controller.SignalCause(), prepared.store, stream,
	)
	if streamErr != nil {
		return finishApplyEncodingOrWriteError(stream, prepared.store, streamErr, failure)
	}
	if failure != nil {
		return finishApplyMachineFailure(stream, prepared.store, failure)
	}
	if err := validateApplyResult(runtimeOutcome.result); err != nil {
		return finishApplyEncodingOrWriteError(
			stream, prepared.store, applyEncodingError(err), nil,
		)
	}
	preparedOutputs, err := prepareApplyOutputs(
		stream.format, runtimeOutcome.result.Outputs,
		rootSensitiveOutputs(prepared.parsed),
	)
	if err != nil {
		return finishApplyEncodingOrWriteError(
			stream, prepared.store, err, nil,
		)
	}
	for _, output := range preparedOutputs {
		if err := stream.Output(output.Name, output.Value, output.Sensitive); err != nil {
			return finishApplyEncodingOrWriteError(
				stream, prepared.store, err, nil,
			)
		}
	}
	if err := stream.Result(runtimeOutcome.result); err != nil {
		return finishApplyEncodingOrWriteError(stream, prepared.store, err, nil)
	}
	return nil
}

func newApplyExecutor(
	info Info,
	prepared *preparedApplyCommand,
	controller *applySignalController,
	events chan<- runtime.ApplyEvent,
) *runtime.Executor {
	return &runtime.Executor{
		SyntaxSource: prepared.parsed.syntaxBody,
		DAG:          prepared.parsed.dag,
		Libraries:    info.Libraries,
		Store:        prepared.store,
		Factory: state.FactoryInfo{
			Name:            info.FactoryName,
			Version:         info.FactoryVersion,
			ContentRevision: info.ContentRevision,
		},
		Parallelism: prepared.parallelism,
		Drain:       controller.Drain(),
		Events:      events,
	}
}

func enrichApplyFailure(
	failure *runtime.ApplyFailure,
	signalCause error,
	store state.Backend,
	stream *applyStream,
) *runtime.ApplyFailure {
	if failure == nil && signalCause != nil {
		failure = runtime.NewApplyFailure(runtime.ApplyFailureExecute, signalCause)
	} else if failure != nil && signalCause != nil && !errors.Is(failure.Cause, signalCause) {
		failure = runtime.NewApplyFailure(
			failure.Stage, errors.Join(signalCause, failure.Cause),
		)
	}
	if failure == nil || store == nil {
		return failure
	}
	revision, err := currentStateRevision(store)
	if err != nil {
		failure = runtime.NewApplyFailure(
			failure.Stage, errors.Join(failure.Cause, err),
		)
		stream.stateRev = nil
		return failure
	}
	stream.stateRev = revision
	return failure
}

func finishApplyMachineFailure(
	stream *applyStream,
	store state.Backend,
	failure *runtime.ApplyFailure,
) error {
	if failure == nil {
		failure = runtime.NewApplyFailure(
			runtime.ApplyFailureFinalize, errors.New("apply failed without a cause"),
		)
	}
	if err := stream.Error(failure); err != nil {
		return finishApplyEncodingOrWriteError(stream, store, err, failure)
	}
	return cmdout.Reported(failure)
}

func finishApplyEncodingOrWriteError(
	stream *applyStream,
	store state.Backend,
	streamErr error,
	ordinary error,
) error {
	var writeErr *applyStreamWriteError
	if errors.As(streamErr, &writeErr) {
		return writeErr.Cause
	}
	var encodingErr *applyStreamEncodingError
	if !errors.As(streamErr, &encodingErr) {
		return streamErr
	}
	cause := encodingErr.Cause
	if ordinary != nil {
		cause = errors.Join(cause, errors.New(ordinary.Error()))
	}
	failure := runtime.NewApplyFailure(runtime.ApplyFailureFinalize, cause)
	failure = enrichApplyFailure(failure, nil, store, stream)
	if err := stream.Error(failure); err != nil {
		var fallbackWrite *applyStreamWriteError
		if errors.As(err, &fallbackWrite) {
			return fallbackWrite.Cause
		}
		var fallbackEncoding *applyStreamEncodingError
		if errors.As(err, &fallbackEncoding) {
			return fallbackEncoding.Cause
		}
		return err
	}
	return cmdout.Reported(streamErr)
}

func startApplyRunView(
	info Info,
	plan *runtime.PlanFile,
	dag *runtime.DAG,
) (applyRunView, error) {
	return ui.Start(ui.Config{
		Factory: info.FactoryName,
		Stack:   plan.Stack,
		Graph:   runtime.PlanGraph(plan, dag),
	})
}

func startApplyBrowser(
	openBrowser func(context.Context, string) error,
	url string,
) <-chan *diagnostic.Diagnostic {
	result := make(chan *diagnostic.Diagnostic, 1)
	ctx, cancel := context.WithTimeout(context.Background(), applyBrowserTimeout)
	go func() {
		defer cancel()
		result <- applyBrowserNotice(openBrowser(ctx, url))
		close(result)
	}()
	return result
}

func applyBrowserNotice(err error) *diagnostic.Diagnostic {
	if err == nil {
		return nil
	}
	message := "No browser opened; use the run view URL to watch."
	if errors.Is(err, context.DeadlineExceeded) {
		message = "Browser open timed out after 5s; use the run view URL to watch."
	}
	return &diagnostic.Diagnostic{
		Code: "unobin.ui.browser-open", Severity: diagnostic.SeverityWarning,
		Message: message,
	}
}
