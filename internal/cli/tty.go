package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"golang.org/x/term"

	"hum/internal/daemon"
	"hum/internal/project"
	"hum/internal/protocol"
)

// ttyInput owns local terminal policy for one attached run. The daemon owns
// the PTY and durable lease; this helper only reads the caller's stdin and
// restores its local mode on every exit path.
type ttyInput struct {
	session  *daemon.InputSession
	stdin    *os.File
	errOut   io.Writer
	terminal bool

	mu       sync.Mutex
	restored bool
	restore  func()
	stop     chan struct{}
	stopOnce sync.Once
	done     chan struct{}
}

func ttyInputRequest(name, root string, definition project.Definition, argv []string) daemon.InputAttachRequest {
	request := daemon.InputAttachRequest{
		Op: protocol.OpInputAttach, Name: name, Cwd: root, Root: root, TTY: true,
		Argv: append([]string(nil), argv...), Source: definition.Source,
		Ready: readinessConfig(definition),
	}
	if len(argv) == 0 {
		request.Argv = append([]string(nil), definition.Argv...)
	}
	if request.Cwd == "" {
		request.Cwd = definition.Cwd
	}
	if request.Source == "" && len(argv) != 0 {
		request.Source = "ad_hoc"
	}
	if stdin := os.Stdin; stdin != nil && term.IsTerminal(int(stdin.Fd())) {
		if width, height, err := term.GetSize(int(stdin.Fd())); err == nil && width > 0 && height > 0 {
			request.Columns, request.Rows = uint16(width), uint16(height)
		}
	}
	return request
}

func isInputConflict(err error) bool {
	var wire *protocol.WireError
	return errors.As(err, &wire) && wire != nil && wire.Code == protocol.ErrorInputConflict
}

func isInputStale(err error) bool {
	var wire *protocol.WireError
	return errors.As(err, &wire) && wire != nil && wire.Code == protocol.ErrorInputStale
}

func isInputStopped(err error) bool {
	var wire *protocol.WireError
	if !errors.As(err, &wire) || wire == nil || wire.Code != protocol.ErrorInputClosed {
		return false
	}
	if details, ok := wire.Details.(map[string]any); ok {
		stopped, _ := details["stopped"].(bool)
		return stopped
	}
	return strings.Contains(strings.ToLower(wire.Message), "incarnation") && strings.Contains(strings.ToLower(wire.Message), "stop")
}

func newTTYInput(session *daemon.InputSession, errOut io.Writer) (*ttyInput, error) {
	if session == nil {
		return nil, errors.New("nil tty input session")
	}
	input := &ttyInput{session: session, stdin: os.Stdin, errOut: errOut, stop: make(chan struct{}), done: make(chan struct{})}
	if input.stdin == nil {
		return input, nil
	}
	input.terminal = term.IsTerminal(int(input.stdin.Fd()))
	if input.terminal {
		state, err := term.MakeRaw(int(input.stdin.Fd()))
		if err != nil {
			return nil, fmt.Errorf("enable raw terminal input: %w", err)
		}
		input.restore = func() { _ = term.Restore(int(input.stdin.Fd()), state) }
	}
	return input, nil
}

func (i *ttyInput) start() {
	if i == nil {
		return
	}
	go i.forward()
	go i.watchSession()
	if i.stdin != nil && term.IsTerminal(int(i.stdin.Fd())) {
		go i.resizeLoop()
	}
}

func (i *ttyInput) watchSession() {
	for {
		if _, err := i.session.Next(context.Background()); err != nil {
			i.detach()
			return
		}
	}
}

func (i *ttyInput) resizeLoop() {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGWINCH)
	defer signal.Stop(signals)
	for {
		select {
		case <-i.stop:
			return
		case <-signals:
			width, height, err := term.GetSize(int(i.stdin.Fd()))
			if err != nil || width <= 0 || height <= 0 {
				continue
			}
			_, cursor := i.session.State()
			_ = i.session.ResizeAt(context.Background(), cursor, uint16(width), uint16(height))
		}
	}
}

func (i *ttyInput) forward() {
	defer close(i.done)
	if i.stdin == nil {
		return
	}
	buffer := make([]byte, 32*1024)
	for {
		count, err := i.stdin.Read(buffer)
		if count > 0 {
			payload := buffer[:count]
			if !i.terminal {
				i.forwardChunk(payload)
				if err != nil {
					if errors.Is(err, io.EOF) {
						i.detach()
					}
					return
				}
				continue
			}
			for {
				index := -1
				for position, value := range payload {
					if value == 0x1d {
						index = position
						break
					}
				}
				if index < 0 {
					i.forwardChunk(payload)
					break
				}
				i.forwardChunk(payload[:index])
				// Ctrl+] is consumed locally and detaches only this input owner.
				i.detach()
				return
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				i.detach()
			}
			return
		}
		select {
		case <-i.stop:
			return
		default:
		}
	}
}

func (i *ttyInput) forwardChunk(payload []byte) {
	if len(payload) == 0 {
		return
	}
	state, cursor := i.session.State()
	if state != "running" {
		// Stopped sessions retain the lease but discard local terminal bytes.
		return
	}
	if err := i.session.WriteAt(context.Background(), cursor, payload); err != nil {
		if isInputStale(err) || isInputStopped(err) {
			return
		}
		if state, current := i.session.State(); state == "stopped" && current == cursor {
			// The process can exit after State but before WriteAt. The daemon
			// rejects that incarnation's bytes while retaining this lease.
			return
		}
		if !errors.Is(err, context.Canceled) {
			i.detach()
		}
	}
}

func (i *ttyInput) detach() {
	if i == nil {
		return
	}
	i.stopOnce.Do(func() {
		close(i.stop)
		// Restore before closing stdin so terminal attributes are recoverable;
		// closing the descriptor also unblocks a reader after detach or transport
		// loss and prevents it from forwarding bytes to a released lease.
		i.restoreLocal()
		if i.stdin != nil {
			_ = i.stdin.Close()
		}
		_ = i.session.Release()
	})
}

func (i *ttyInput) restoreLocal() {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.restored {
		return
	}
	i.restored = true
	if i.restore != nil {
		i.restore()
	}
}

func (i *ttyInput) close() {
	if i == nil {
		return
	}
	i.detach()
}
