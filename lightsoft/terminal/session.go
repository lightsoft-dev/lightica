package main

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/creack/pty"
)

// hangupGrace is how long processes get to exit after SIGHUP before the
// session's remaining processes are killed.
const hangupGrace = 5 * time.Second

// shellSession is one login shell on its own pseudo-terminal.
type shellSession struct {
	cmd     *exec.Cmd
	ptmx    *os.File
	ttyName string
	exited  chan struct{}
	exitErr error

	lastActivity atomic.Int64
	stopOnce     sync.Once
}

func startShell(shell, dir string, env []string, cols, rows uint16) (*shellSession, error) {
	ptmx, tty, err := pty.Open()
	if err != nil {
		return nil, err
	}
	if err := pty.Setsize(ptmx, &pty.Winsize{Cols: cols, Rows: rows}); err != nil {
		ptmx.Close()
		tty.Close()
		return nil, err
	}
	cmd := exec.Command(shell, "-l")
	cmd.Dir = dir
	cmd.Env = env
	cmd.Stdin, cmd.Stdout, cmd.Stderr = tty, tty, tty
	// A new session with the pty as its controlling terminal: the shell does
	// job control, and every process it starts stays findable by tty.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true}
	if err := cmd.Start(); err != nil {
		ptmx.Close()
		tty.Close()
		return nil, err
	}
	ttyName := tty.Name()
	tty.Close()

	s := &shellSession{cmd: cmd, ptmx: ptmx, ttyName: ttyName, exited: make(chan struct{})}
	s.touch()
	go func() {
		s.exitErr = cmd.Wait()
		close(s.exited)
	}()
	return s, nil
}

func (s *shellSession) touch() { s.lastActivity.Store(time.Now().UnixNano()) }

func (s *shellSession) idleFor(now time.Time) time.Duration {
	return now.Sub(time.Unix(0, s.lastActivity.Load()))
}

func (s *shellSession) resize(cols, rows uint16) error {
	return pty.Setsize(s.ptmx, &pty.Winsize{Cols: cols, Rows: rows})
}

func (s *shellSession) exitCode() int {
	var ee *exec.ExitError
	if errors.As(s.exitErr, &ee) {
		return ee.ExitCode()
	}
	if s.exitErr != nil {
		return -1
	}
	return 0
}

// stop hangs up every process on the session's terminal, as closing a
// terminal window would, then kills whatever is still attached after the
// grace period. Processes the user deliberately detached (setsid, nohup with
// disown) keep running, as they would after closing a local terminal.
func (s *shellSession) stop() {
	s.stopOnce.Do(func() {
		signalAll(s.targets(), syscall.SIGHUP)
		select {
		case <-s.exited:
		case <-time.After(hangupGrace):
		}
		signalAll(s.targets(), syscall.SIGKILL)
		select {
		case <-s.exited:
		case <-time.After(hangupGrace):
		}
		s.ptmx.Close()
	})
}

// targets lists the processes still attached to the terminal, plus the
// shell's process group while the shell is unreaped (after that its id may be
// reused).
func (s *shellSession) targets() []int {
	pids := ttyPIDs(s.ttyName)
	select {
	case <-s.exited:
	default:
		pids = append(pids, -s.cmd.Process.Pid)
	}
	return pids
}

func signalAll(pids []int, sig syscall.Signal) {
	for _, pid := range pids {
		_ = syscall.Kill(pid, sig)
	}
}

// ttyPIDs lists processes whose controlling terminal is the given tty,
// including background jobs the shell placed in other process groups.
func ttyPIDs(ttyName string) []int {
	out, err := exec.Command("ps", "-t", strings.TrimPrefix(ttyName, "/dev/"), "-o", "pid=").Output()
	if err != nil {
		return nil
	}
	var pids []int
	for _, f := range strings.Fields(string(out)) {
		if pid, err := strconv.Atoi(f); err == nil && pid > 1 {
			pids = append(pids, pid)
		}
	}
	return pids
}

// shellEnv builds the shell's environment from the gateway's own, without
// Multica credentials or gateway settings.
func shellEnv(base []string, home, shell, dir, user string) []string {
	out := make([]string, 0, len(base)+8)
	set := map[string]bool{}
	for _, kv := range base {
		k, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(k, "MULTICA_") || strings.HasPrefix(k, "LIGHTSOFT_TERM_") ||
			// Variables describing the gateway's own parent terminal; with
			// TERM_PROGRAM=Apple_Terminal zsh would restore that app's sessions.
			strings.HasPrefix(k, "TERM_") || strings.HasPrefix(k, "ITERM_") {
			continue
		}
		switch k {
		case "TERM", "COLORTERM", "PWD", "OLDPWD", "SHLVL", "LIGHTSOFT_TERMINAL_USER":
			continue
		}
		set[k] = true
		out = append(out, kv)
	}
	defaults := [][2]string{
		{"HOME", home},
		{"SHELL", shell},
		{"LANG", "en_US.UTF-8"},
		{"PATH", "/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"},
	}
	for _, d := range defaults {
		if !set[d[0]] {
			out = append(out, d[0]+"="+d[1])
		}
	}
	return append(out,
		"PWD="+dir,
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
		"LIGHTSOFT_TERMINAL_USER="+user,
	)
}
