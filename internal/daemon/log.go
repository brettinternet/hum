package daemon

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

// boundedLog is a small append-only diagnostic sink. Rotation happens before a
// write that would exceed the configured bound, so the active file is always
// bounded even when a single diagnostic is unusually large.
type boundedLog struct {
	mu      sync.Mutex
	path    string
	max     int64
	file    *os.File
	size    int64
	rotated string
}

func openBoundedLog(path string, max int64) (*boundedLog, error) {
	if max <= 0 {
		max = defaultLogBytes
	}
	if max > maxLogBytes {
		max = maxLogBytes
	}
	log := &boundedLog{path: path, max: max, rotated: path + ".1"}
	if err := log.open(); err != nil {
		return nil, err
	}
	return log, nil
}

func (l *boundedLog) open() error {
	if err := os.MkdirAll(filepath.Dir(l.path), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return err
	}
	l.file = file
	l.size = info.Size()
	if l.size > l.max {
		if err := l.rotateLocked(); err != nil {
			_ = file.Close()
			l.file = nil
			return err
		}
	}
	return nil
}

func (l *boundedLog) rotateLocked() error {
	if l.file != nil {
		if err := l.file.Close(); err != nil {
			return err
		}
		l.file = nil
	}
	_ = os.Remove(l.rotated)
	if err := os.Rename(l.path, l.rotated); err != nil && !os.IsNotExist(err) {
		return err
	}
	file, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return err
	}
	l.file = file
	l.size = 0
	return nil
}

func (l *boundedLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return 0, os.ErrClosed
	}
	if l.size > 0 && l.size+int64(len(p)) > l.max {
		if err := l.rotateLocked(); err != nil {
			return 0, err
		}
	}
	// Keep one active file bounded even if one record exceeds max. Retaining
	// the tail is more useful than allowing an unbounded diagnostic write.
	if int64(len(p)) > l.max {
		p = p[len(p)-int(l.max):]
	}
	n, err := l.file.Write(p)
	l.size += int64(n)
	return n, err
}

func (l *boundedLog) Printf(format string, args ...any) {
	_, _ = io.WriteString(l, fmt.Sprintf(format, args...))
}

func (l *boundedLog) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return nil
	}
	err := l.file.Close()
	l.file = nil
	return err
}
