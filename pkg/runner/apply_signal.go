package runner

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/cloudboss/unobin/pkg/diagnostic"
	"github.com/cloudboss/unobin/pkg/runtime"
)

const applyDrainGrace = 60 * time.Second

const applyDrainMessage = "Interrupted; letting in-flight steps finish. " +
	"Press Ctrl-C again or send SIGTERM to abort."

type applySignalController struct {
	ctx        context.Context
	cancel     context.CancelCauseFunc
	drain      chan struct{}
	notices    chan diagnostic.Diagnostic
	signals    <-chan os.Signal
	grace      time.Duration
	after      func(time.Duration) <-chan time.Time
	stop       chan struct{}
	done       chan struct{}
	stopOnce   sync.Once
	stopNotify func()

	mu          sync.Mutex
	signalCause error
}

func newApplySignalController(
	parent context.Context,
	signals <-chan os.Signal,
	grace time.Duration,
	after func(time.Duration) <-chan time.Time,
) *applySignalController {
	if parent == nil {
		parent = context.Background()
	}
	if after == nil {
		after = time.After
	}
	ctx, cancel := context.WithCancelCause(parent)
	controller := &applySignalController{
		ctx: ctx, cancel: cancel, drain: make(chan struct{}),
		notices: make(chan diagnostic.Diagnostic, 1), signals: signals,
		grace: grace, after: after, stop: make(chan struct{}), done: make(chan struct{}),
	}
	go controller.run()
	return controller
}

func newSystemApplySignalController(parent context.Context) *applySignalController {
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	controller := newApplySignalController(
		parent, signals, applyDrainGrace, time.After,
	)
	controller.stopNotify = func() {
		signal.Stop(signals)
	}
	return controller
}

func (c *applySignalController) Context() context.Context {
	return c.ctx
}

func (c *applySignalController) Drain() <-chan struct{} {
	return c.drain
}

func (c *applySignalController) Notices() <-chan diagnostic.Diagnostic {
	return c.notices
}

func (c *applySignalController) Cancel(cause error) {
	if cause == nil {
		cause = context.Canceled
	}
	c.cancel(cause)
}

func (c *applySignalController) SignalCause() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.signalCause
}

func (c *applySignalController) Stop() {
	c.stopOnce.Do(func() {
		if c.stopNotify != nil {
			c.stopNotify()
		}
		close(c.stop)
	})
	<-c.done
	c.cancel(context.Canceled)
}

func (c *applySignalController) run() {
	defer close(c.done)
	defer close(c.notices)
	var grace <-chan time.Time
	draining := false
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-c.stop:
			return
		case <-grace:
			c.interrupt()
			return
		case value, ok := <-c.signals:
			if !ok {
				return
			}
			switch value {
			case syscall.SIGINT:
				if draining {
					c.interrupt()
					return
				}
				draining = true
				close(c.drain)
				c.notices <- diagnostic.Diagnostic{
					Code: "unobin.apply.drain-requested", Severity: diagnostic.SeverityInfo,
					Message: applyDrainMessage,
				}
				grace = c.after(c.grace)
			case syscall.SIGTERM:
				c.interrupt()
				return
			}
		}
	}
}

func (c *applySignalController) interrupt() {
	c.mu.Lock()
	c.signalCause = runtime.ErrInterrupted
	c.mu.Unlock()
	c.cancel(runtime.ErrInterrupted)
}
