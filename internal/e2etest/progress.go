package e2etest

import (
	"fmt"
	"os"
	"testing"
)

type progressLogger interface {
	Helper()
	Logf(format string, args ...any)
}

var writeLiveProgress = writeTTYProgress

func logProgress(t progressLogger, format string, args ...any) {
	t.Helper()
	message := fmt.Sprintf("e2e: "+format, args...)
	if writeLiveProgress(message) {
		return
	}
	t.Logf("%s", message)
}

func writeTTYProgress(message string) bool {
	if !testing.Verbose() && os.Getenv("UNOBIN_E2E_PROGRESS") == "" {
		return false
	}
	tty, err := os.OpenFile("/dev/tty", os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return false
	}
	defer func() { _ = tty.Close() }()
	_, err = fmt.Fprintln(tty, message)
	return err == nil
}

func setLiveProgressWriterForTest(write func(string) bool) func() {
	previous := writeLiveProgress
	writeLiveProgress = write
	return func() { writeLiveProgress = previous }
}
