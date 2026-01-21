package gekko

import (
	"fmt"
	"log"
	"os"
	"sync"
)

type Logger interface {
	DebugEnabled() bool
	SetDebug(enabled bool)
	Debugf(format string, args ...any)
	Infof(format string, args ...any)
	Warnf(format string, args ...any)
	Errorf(format string, args ...any)
}

type DefaultLogger struct {
	mu     sync.Mutex
	debug  bool
	prefix string
	out    *log.Logger
	err    *log.Logger
}

func NewDefaultLogger(prefix string, debug bool) *DefaultLogger {
	flags := log.LstdFlags | log.Lmicroseconds
	return &DefaultLogger{
		debug:  debug,
		prefix: prefix,
		out:    log.New(os.Stdout, "", flags),
		err:    log.New(os.Stderr, "", flags),
	}
}

func (l *DefaultLogger) DebugEnabled() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.debug
}

func (l *DefaultLogger) SetDebug(enabled bool) {
	l.mu.Lock()
	l.debug = enabled
	l.mu.Unlock()
}

func (l *DefaultLogger) prefixf(level string, format string, args ...any) string {
	if l.prefix != "" {
		return fmt.Sprintf("[%s] %s: %s", l.prefix, level, fmt.Sprintf(format, args...))
	}
	return fmt.Sprintf("%s: %s", level, fmt.Sprintf(format, args...))
}

func (l *DefaultLogger) Debugf(format string, args ...any) {
	l.mu.Lock()
	dbg := l.debug
	l.mu.Unlock()
	if !dbg {
		return
	}
	l.out.Print(l.prefixf("DEBUG", format, args...))
}

func (l *DefaultLogger) Infof(format string, args ...any) {
	l.out.Print(l.prefixf("INFO", format, args...))
}

func (l *DefaultLogger) Warnf(format string, args ...any) {
	l.err.Print(l.prefixf("WARN", format, args...))
}

func (l *DefaultLogger) Errorf(format string, args ...any) {
	l.err.Print(l.prefixf("ERROR", format, args...))
}

// LoggingModule installs a default logger as a resource.
type LoggingModule struct {
	Prefix string
	Debug  bool
}

func (m LoggingModule) Install(app *App, cmd *Commands) {
	logger := NewDefaultLogger(m.Prefix, m.Debug)
	app.addResources(logger)
}
// Nop logger and App helper accessor

type nopLogger struct{}

func NewNopLogger() Logger { return &nopLogger{} }
func (n *nopLogger) DebugEnabled() bool                     { return false }
func (n *nopLogger) SetDebug(enabled bool)                  {}
func (n *nopLogger) Debugf(format string, args ...any)      {}
func (n *nopLogger) Infof(format string, args ...any)       {}
func (n *nopLogger) Warnf(format string, args ...any)       {}
func (n *nopLogger) Errorf(format string, args ...any)      {}

// Logger returns the first Logger resource if present, otherwise a no-op logger.
// Safe to call at any time; never returns nil.
func (app *App) Logger() Logger {
	if app == nil {
		return NewNopLogger()
	}
	if app.resources != nil {
		for _, r := range app.resources {
			if l, ok := r.(Logger); ok {
				return l
			}
		}
	}
	return NewNopLogger()
}