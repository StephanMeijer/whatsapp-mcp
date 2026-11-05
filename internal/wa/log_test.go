// SPDX-License-Identifier: MPL-2.0

package wa

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestLoggerAdapter(t *testing.T) {
	var buf bytes.Buffer
	h := slog.NewTextHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelInfo,
		// Drop the time so lines are stable.
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				return slog.Attr{}
			}
			return a
		},
	})
	l := newLogger(slog.New(h)).Sub("client")

	l.Debugf("hidden %d", 1)
	l.Infof("info %d", 2)
	l.Warnf("warn %s", "three")
	l.Errorf("error %v", 4)

	want := strings.Join([]string{
		`level=INFO msg="info 2" module=client`,
		`level=WARN msg="warn three" module=client`,
		`level=ERROR msg="error 4" module=client`,
		``,
	}, "\n")
	if got := buf.String(); got != want {
		t.Errorf("log output:\n%s\nwant:\n%s", got, want)
	}
}
