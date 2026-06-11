package ui

import (
	"os"
	"os/exec"
	"runtime"
	"time"
)

// OpenBrowser tries to open url in the operator's browser and reports
// whether a command appeared to succeed. $BROWSER wins, then the
// platform opener, then a short list of common browsers.
func OpenBrowser(url string) bool {
	for _, args := range browserCommands() {
		cmd := exec.Command(args[0], append(args[1:], url)...)
		if cmd.Start() == nil && appearsSuccessful(cmd, 3*time.Second) {
			return true
		}
	}
	return false
}

func browserCommands() [][]string {
	var cmds [][]string
	if b := os.Getenv("BROWSER"); b != "" {
		cmds = append(cmds, []string{b})
	}
	switch runtime.GOOS {
	case "darwin":
		cmds = append(cmds, []string{"open"})
	case "windows":
		cmds = append(cmds, []string{"cmd", "/c", "start"})
	default:
		if os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != "" {
			cmds = append(cmds, []string{"xdg-open"})
		}
	}
	return append(cmds,
		[]string{"chrome"},
		[]string{"google-chrome"},
		[]string{"chromium"},
		[]string{"firefox"},
	)
}

// appearsSuccessful reports whether the command exits cleanly within
// the timeout or is still running when it expires, which is how a
// browser that stays in the foreground looks.
func appearsSuccessful(cmd *exec.Cmd, timeout time.Duration) bool {
	errc := make(chan error, 1)
	go func() { errc <- cmd.Wait() }()
	select {
	case <-time.After(timeout):
		return true
	case err := <-errc:
		return err == nil
	}
}
