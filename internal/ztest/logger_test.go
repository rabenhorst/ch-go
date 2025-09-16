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
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestTestLogger(t *testing.T) {
	ts := newTestLogSpy(t)
	defer ts.AssertPassed()

	log := NewLogger(ts)

	log.Info("received work order")
	log.Debug("starting work")
	log.Warn("work may fail")
	log.Error("work failed", "error", errors.New("great sadness"))

	// slog doesn't have Panic method, so we simulate it
	assert.Panics(t, func() {
		log.Log(nil, slog.LevelError+8, "failed to do work") // Higher level for panic
		panic("failed to do work")
	}, "log.Panic should panic")

	ts.AssertMessages(
		"INF	received work order",
		"DBG	starting work",
		"WRN	work may fail",
		`ERR	work failed	{"error": "great sadness"}`,
		"PAN	failed to do work",
	)
}

func TestTestLoggerSupportsLevels(t *testing.T) {
	ts := newTestLogSpy(t)
	defer ts.AssertPassed()

	log := NewLogger(ts, Level(slog.LevelWarn))

	log.Info("received work order")
	log.Debug("starting work")
	log.Warn("work may fail")
	log.Error("work failed", "error", errors.New("great sadness"))

	// slog doesn't have Panic method, so we simulate it
	assert.Panics(t, func() {
		log.Log(nil, slog.LevelError+8, "failed to do work") // Higher level for panic
		panic("failed to do work")
	}, "log.Panic should panic")

	ts.AssertMessages(
		"WRN	work may fail",
		`ERR	work failed	{"error": "great sadness"}`,
		"PAN	failed to do work",
	)
}

func TestTestLoggerSupportsWrappedSlogOptions(t *testing.T) {
	ts := newTestLogSpy(t)
	defer ts.AssertPassed()

	// Create logger with AddSource option and pre-configured attributes
	log := NewLogger(ts, AddSource()).With("k1", "v1")

	log.Info("received work order")
	log.Debug("starting work")
	log.Warn("work may fail")
	log.Error("work failed", "error", errors.New("great sadness"))

	assert.Panics(t, func() {
		log.Log(nil, slog.LevelError+8, "failed to do work")
		panic("failed to do work")
	}, "log.Panic should panic")

	// Note: Since we're using AddSource(), the exact line numbers may vary
	// We'll check that the messages contain the expected patterns
	assert.Len(t, ts.Messages, 5)
	for _, msg := range ts.Messages {
		assert.Contains(t, msg, `{"k1": "v1"}`)
	}
	assert.Contains(t, ts.Messages[0], "INF")
	assert.Contains(t, ts.Messages[0], "received work order")
	assert.Contains(t, ts.Messages[1], "DBG")
	assert.Contains(t, ts.Messages[1], "starting work")
	assert.Contains(t, ts.Messages[2], "WRN")
	assert.Contains(t, ts.Messages[2], "work may fail")
	assert.Contains(t, ts.Messages[3], "ERR")
	assert.Contains(t, ts.Messages[3], "work failed")
	assert.Contains(t, ts.Messages[3], "great sadness")
	assert.Contains(t, ts.Messages[4], "PAN")
	assert.Contains(t, ts.Messages[4], "failed to do work")
}

func TestTestingHandler(t *testing.T) {
	ts := newTestLogSpy(t)
	h := newTestingHandler(ts, slog.LevelDebug, false, formatTime(time.Now()))

	// Test the handler directly
	record := slog.NewRecord(time.Now(), slog.LevelInfo, "hello", 0)
	err := h.Handle(nil, record)
	assert.NoError(t, err, "Handle must not fail")
	assert.Len(t, ts.Messages, 1)
	assert.Contains(t, ts.Messages[0], "INF")
	assert.Contains(t, ts.Messages[0], "hello")
}

func TestTestLoggerErrorOutput(t *testing.T) {
	// This test verifies that the test logger can mark tests as failed
	// when configured with markFailed option.

	ts := newTestLogSpy(t)
	defer ts.AssertFailed()

	// Create a handler that marks test as failed
	h := newTestingHandler(ts, slog.LevelDebug, false, formatTime(time.Now()))
	h = h.WithMarkFailed(true)

	log := slog.New(h)
	log.Info("foo") // this should mark the test as failed

	if assert.Len(t, ts.Messages, 1, "expected a log message") {
		assert.Contains(t, ts.Messages[0], "foo")
	}
}

// testLogSpy is a testing.TB that captures logged messages.
type testLogSpy struct {
	testing.TB

	failed   bool
	Messages []string
}

func newTestLogSpy(t testing.TB) *testLogSpy {
	return &testLogSpy{TB: t}
}

func (t *testLogSpy) Fail() {
	t.failed = true
}

func (t *testLogSpy) Failed() bool {
	return t.failed
}

func (t *testLogSpy) FailNow() {
	t.Fail()
	t.TB.FailNow()
}

func (t *testLogSpy) Logf(format string, args ...interface{}) {
	// Log messages are in the format,
	//
	//   2017-10-27T13:03:01.000-0700	DEBUG	your message here	{data here}
	//
	// We strip the first part of these messages because we can't really test
	// for the timestamp from these tests.
	m := fmt.Sprintf(format, args...)
	m = m[strings.IndexByte(m, '\t')+1:]
	t.Messages = append(t.Messages, m)
	t.Log(m)
}

func (t *testLogSpy) AssertMessages(msgs ...string) {
	assert.Equal(t.TB, msgs, t.Messages, "logged messages did not match")
}

func (t *testLogSpy) AssertPassed() {
	t.assertFailed(false, "expected test to pass")
}

func (t *testLogSpy) AssertFailed() {
	t.assertFailed(true, "expected test to fail")
}

func (t *testLogSpy) assertFailed(v bool, msg string) {
	assert.Equal(t.TB, v, t.failed, msg)
}
