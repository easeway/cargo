package cargo

import (
	"github.com/op/go-logging"
	"io"
	"strings"
)

type goLogger struct {
	Logger  *logging.Logger
	Backend logging.LeveledBackend
}

func GoLogger(name string, backend logging.LeveledBackend) Logger {
	logger := logging.MustGetLogger(name)
	logger.SetBackend(backend)
	return &goLogger{Logger: logger, Backend: backend}
}

func (l *goLogger) NewLogger(name string) Logger {
	return GoLogger(name, l.Backend)
}

func (l *goLogger) Critical(fmt string, args ...interface{}) {
	l.Logger.Critical(fmt, args...)
}

func (l *goLogger) Error(fmt string, args ...interface{}) {
	l.Logger.Error(fmt, args...)
}

func (l *goLogger) Warning(fmt string, args ...interface{}) {
	l.Logger.Warning(fmt, args...)
}

func (l *goLogger) Info(fmt string, args ...interface{}) {
	l.Logger.Notice(fmt, args...)
}

func (l *goLogger) Debug(fmt string, args ...interface{}) {
	l.Logger.Info(fmt, args...)
}

func (l *goLogger) Trace(fmt string, args ...interface{}) {
	l.Logger.Debug(fmt, args...)
}

type logWriter struct {
	logger Logger
	prefix string
}

func LoggerWriter(logger Logger, prefix string) io.Writer {
	return &logWriter{logger: logger, prefix: prefix}
}

func (lw *logWriter) Write(p []byte) (int, error) {
	for _, line := range strings.Split(string(p), "\n") {
		lw.logger.Trace("%s%s", lw.prefix, line)
	}
	return len(p), nil
}
