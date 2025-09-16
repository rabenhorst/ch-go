// Copyright (c) 2017 Uber Technologies, Inc.
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.

package ztest

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"
)

// LoggerOption configures the test logger built by NewLogger.
type LoggerOption interface {
	applyLoggerOption(*loggerOptions)
}

type loggerOptions struct {
	Level       slog.Level
	slogOptions []func(*slog.HandlerOptions)
	addSource   bool
}

type loggerOptionFunc func(*loggerOptions)

func (f loggerOptionFunc) applyLoggerOption(opts *loggerOptions) {
	f(opts)
}

// Level controls which messages are logged by a test Logger built by
// NewLogger.
func Level(level slog.Level) LoggerOption {
	return loggerOptionFunc(func(opts *loggerOptions) {
		opts.Level = level
	})
}

// WrapOptions adds slog handler configuration to a test Logger built by NewLogger.
func WrapOptions(slogOpts ...func(*slog.HandlerOptions)) LoggerOption {
	return loggerOptionFunc(func(opts *loggerOptions) {
		opts.slogOptions = slogOpts
	})
}

// AddSource adds source information to log entries.
func AddSource() LoggerOption {
	return loggerOptionFunc(func(opts *loggerOptions) {
		opts.addSource = true
	})
}

func shortLevel(l slog.Level) string {
	switch {
	case l == slog.LevelDebug:
		return "DBG"
	case l == slog.LevelInfo:
		return "INF"
	case l == slog.LevelWarn:
		return "WRN"
	case l == slog.LevelError:
		return "ERR"
	case l >= slog.LevelError+4: // Panic-like level
		return "PAN"
	default:
		return "UNK"
	}
}

// formatLevel formats level as a short string for display.
func formatLevel(l slog.Level) string {
	return shortLevel(l)
}

func formatTime(start time.Time) func(time.Time) string {
	return func(t time.Time) string {
		elapsed := time.Since(start).Round(time.Millisecond).Seconds()
		return fmt.Sprintf("%4.2f", elapsed)
	}
}

// NewLogger builds a new Logger that logs all messages to the given
// testing.TB.
//
//	logger := ztest.NewLogger(t)
//
// Use this with a *testing.T or *testing.B to get logs which get printed only
// if a test fails or if you ran go test -v.
//
// The returned logger defaults to logging debug level messages and above.
// This may be changed by passing a ztest.Level during construction.
//
//	logger := ztest.NewLogger(t, ztest.Level(slog.LevelWarn))
//
// You may also pass handler options to customize the test logger.
//
//	logger := ztest.NewLogger(t, ztest.AddSource())
func NewLogger(t TestingT, opts ...LoggerOption) *slog.Logger {
	timeFormatter := formatTime(time.Now())

	cfg := loggerOptions{
		Level: slog.LevelDebug,
	}
	for _, o := range opts {
		o.applyLoggerOption(&cfg)
	}

	handler := newTestingHandler(t, cfg.Level, cfg.addSource, timeFormatter)

	// Apply any handler options
	for _, optFunc := range cfg.slogOptions {
		handlerOpts := &slog.HandlerOptions{
			Level:     cfg.Level,
			AddSource: cfg.addSource,
		}
		optFunc(handlerOpts)
		// Update handler with new options
		handler.level = handlerOpts.Level.Level()
		handler.addSource = handlerOpts.AddSource
	}

	return slog.New(handler)
}

// TestingT is a subset of testing.TB interface needed by the test logger.
type TestingT interface {
	Logf(format string, args ...interface{})
	Fail()
}

// testingHandler is a custom slog.Handler that writes to testing.TB.
type testingHandler struct {
	t          TestingT
	level      slog.Level
	markFailed bool
	addSource  bool
	timeFormat func(time.Time) string
	attrs      []slog.Attr
	groups     []string
}

func newTestingHandler(t TestingT, level slog.Level, addSource bool, timeFormat func(time.Time) string) *testingHandler {
	return &testingHandler{
		t:          t,
		level:      level,
		addSource:  addSource,
		timeFormat: timeFormat,
	}
}

// WithMarkFailed returns a copy of this handler with markFailed set to the provided value.
func (h *testingHandler) WithMarkFailed(v bool) *testingHandler {
	newHandler := *h
	newHandler.markFailed = v
	return &newHandler
}

func (h *testingHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *testingHandler) Handle(ctx context.Context, record slog.Record) error {
	var buf bytes.Buffer

	// Format: TIME\tLEVEL\tMESSAGE\t{ATTRS}
	buf.WriteString(h.timeFormat(record.Time))
	buf.WriteByte('\t')
	buf.WriteString(formatLevel(record.Level))
	buf.WriteByte('\t')

	if h.addSource && record.PC != 0 {
		frame := record.Source()
		if frame.File != "" {
			// Show only the base filename and directory for readability
			relPath := filepath.Base(filepath.Dir(frame.File)) + "/" + filepath.Base(frame.File)
			buf.WriteString(fmt.Sprintf("%s:%d\t", relPath, frame.Line))
		}
	}

	buf.WriteString(record.Message)

	// Collect attributes
	var attrs []slog.Attr
	attrs = append(attrs, h.attrs...) // handler-level attrs
	record.Attrs(func(a slog.Attr) bool {
		attrs = append(attrs, a)
		return true
	})

	if len(attrs) > 0 {
		buf.WriteByte('\t')
		h.formatAttrs(&buf, attrs)
	}

	// Log to testing.TB
	h.t.Logf("%s", buf.String())
	if h.markFailed {
		h.t.Fail()
	}

	return nil
}

func (h *testingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	newHandler := *h
	newHandler.attrs = append(h.attrs, attrs...)
	return &newHandler
}

func (h *testingHandler) WithGroup(name string) slog.Handler {
	newHandler := *h
	newHandler.groups = append(h.groups, name)
	return &newHandler
}

func (h *testingHandler) formatAttrs(buf *bytes.Buffer, attrs []slog.Attr) {
	buf.WriteByte('{')
	for i, attr := range attrs {
		if i > 0 {
			buf.WriteString(", ")
		}
		buf.WriteByte('"')
		buf.WriteString(attr.Key)
		buf.WriteString("\": ")
		h.formatValue(buf, attr.Value)
	}
	buf.WriteByte('}')
}

func (h *testingHandler) formatValue(buf *bytes.Buffer, v slog.Value) {
	switch v.Kind() {
	case slog.KindString:
		buf.WriteByte('"')
		buf.WriteString(v.String())
		buf.WriteByte('"')
	case slog.KindInt64:
		buf.WriteString(fmt.Sprintf("%d", v.Int64()))
	case slog.KindFloat64:
		buf.WriteString(fmt.Sprintf("%g", v.Float64()))
	case slog.KindBool:
		if v.Bool() {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
	default:
		buf.WriteByte('"')
		buf.WriteString(v.String())
		buf.WriteByte('"')
	}
}
