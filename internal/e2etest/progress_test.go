package e2etest

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

type progressRecorder struct {
	messages []string
}

func (r *progressRecorder) Helper() {}

func (r *progressRecorder) Logf(format string, args ...any) {
	r.messages = append(r.messages, fmt.Sprintf(format, args...))
}

func TestLogProgressUsesLiveWriter(t *testing.T) {
	var messages []string
	restore := setLiveProgressWriterForTest(func(message string) bool {
		messages = append(messages, message)
		return true
	})
	defer restore()
	recorder := &progressRecorder{}

	logProgress(recorder, "%s %d", "step", 1)

	require.Equal(t, []string{"e2e: step 1"}, messages)
	require.Empty(t, recorder.messages)
}

func TestLogProgressFallsBackToTestLog(t *testing.T) {
	restore := setLiveProgressWriterForTest(func(string) bool { return false })
	defer restore()
	recorder := &progressRecorder{}

	logProgress(recorder, "%s %d", "step", 1)

	require.Equal(t, []string{"e2e: step 1"}, recorder.messages)
}
