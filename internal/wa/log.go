// SPDX-License-Identifier: MPL-2.0

package wa

import (
	"context"
	"fmt"
	"log/slog"

	waLog "go.mau.fi/whatsmeow/util/log"
)

// slogLogger adapts slog to whatsmeow's printf-style logger.
type slogLogger struct{ l *slog.Logger }

func newLogger(l *slog.Logger) waLog.Logger { return slogLogger{l} }

func (s slogLogger) logf(level slog.Level, msg string, args []any) {
	if s.l.Enabled(context.Background(), level) {
		s.l.Log(context.Background(), level, fmt.Sprintf(msg, args...))
	}
}

func (s slogLogger) Errorf(msg string, args ...any) { s.logf(slog.LevelError, msg, args) }
func (s slogLogger) Warnf(msg string, args ...any)  { s.logf(slog.LevelWarn, msg, args) }
func (s slogLogger) Infof(msg string, args ...any)  { s.logf(slog.LevelInfo, msg, args) }
func (s slogLogger) Debugf(msg string, args ...any) { s.logf(slog.LevelDebug, msg, args) }
func (s slogLogger) Sub(module string) waLog.Logger {
	return slogLogger{s.l.With("module", module)}
}
