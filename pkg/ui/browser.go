package ui

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"time"
)

// OpenBrowserContext tries to open url in the operator's browser. $BROWSER
// wins, then the platform opener, then a short list of common browsers.
func OpenBrowserContext(ctx context.Context, url string) error {
	return openBrowserContext(ctx, url, browserCommands(), 3*time.Second)
}

func openBrowserContext(
	ctx context.Context,
	url string,
	commands [][]string,
	successWait time.Duration,
) error {
	for _, args := range commands {
		if err := ctx.Err(); err != nil {
			return err
		}
		commandArgs := append([]string{}, args[1:]...)
		commandArgs = append(commandArgs, url)
		cmd := exec.Command(args[0], commandArgs...)
		if cmd.Start() == nil && appearsSuccessfulContext(ctx, cmd, successWait) == nil {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	return errors.New("no browser command succeeded")
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

// appearsSuccessfulContext reports nil when the command exits cleanly within
// the timeout or is still running when it expires, which is how a browser
// that stays in the foreground looks.
func appearsSuccessfulContext(
	ctx context.Context,
	cmd *exec.Cmd,
	timeout time.Duration,
) error {
	errc := make(chan error, 1)
	go func() { errc <- cmd.Wait() }()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		<-errc
		return ctx.Err()
	case <-timer.C:
		return nil
	case err := <-errc:
		return err
	}
}
