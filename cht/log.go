package cht

import (
	"bufio"
	"io"
	"log/slog"
	"strconv"
	"strings"
)

type logInfo struct {
	Addr  string
	Ready bool
}

// cut field between start and end, trimming space.
//
// E.g. cut("[ 12345 ]", '[', ']') == "12345".
func cut(s string, start, end byte) string {
	left := strings.IndexByte(s, start)
	if left < 0 {
		return ""
	}
	s = s[left+1:]
	right := strings.IndexByte(s, end)
	if right < 0 {
		return ""
	}
	return strings.TrimSpace(s[:right])
}

type LogEntry struct {
	QueryID  string // f9464441-7023-4df5-89e5-8d16ea6aa2dd
	Severity string // "Debug", "Information", "Trace"
	Name     string // "MemoryTracker", "executeQuery"
	Message  string // "Peak memory usage (for query): 0.00 B."
	ThreadID uint64 // 591781
}

func (e LogEntry) Level() slog.Level {
	switch e.Severity {
	case "Debug", "Trace":
		return slog.LevelDebug
	case "Information":
		return slog.LevelInfo
	case "Warning":
		return slog.LevelWarn
	case "Error":
		return slog.LevelError
	case "Fatal":
		return slog.LevelError // slog doesn't have Fatal, use Error
	default:
		return slog.LevelDebug
	}
}

func parseLog(s string) LogEntry {
	s = strings.TrimSpace(s)
	tid, _ := strconv.ParseUint(cut(s, '[', ']'), 10, 64)
	var textStart int
	if idx := strings.IndexByte(s, '}'); idx > 0 {
		textStart = strings.IndexByte(s[idx:], ':') + idx + 1
	}
	if textStart-1 > len(s) {
		textStart = 0
	}
	return LogEntry{
		QueryID:  cut(s, '{', '}'),
		Severity: cut(s, '<', '>'),
		Name:     cut(s, '>', ':'),
		Message:  strings.TrimSpace(s[textStart:]),
		ThreadID: tid,
	}
}

// logProxy returns io.Writer that can be used as mongo log output.
//
// The io.Writer will parse json logs and write them to provided logger.
// Call context.CancelFunc on mongo exit.
func logProxy(lg *slog.Logger, f func(info logInfo)) io.Writer {
	r, w := io.Pipe()

	s := bufio.NewScanner(r)

	go func() {
		for s.Scan() {
			e := parseLog(s.Text())

			if lg.Enabled(nil, e.Level()) {
				args := []any{}
				if e.QueryID != "" {
					args = append(args, "qid", e.QueryID)
				}
				if e.ThreadID != 0 {
					// Using "pid" to be consistent with ClickHouse log, e.g.:
					// "Will watch for the process with pid 260134"
					args = append(args, "pid", e.ThreadID)
				}
				if e.Name != "" {
					args = append(args, "name", e.Name)
				}
				lg.Log(nil, e.Level(), e.Message, args...)
			}

			if strings.Contains(e.Message, "Ready for connections") {
				f(logInfo{Ready: true})
			}
			if !strings.Contains(e.Message, "Listening for") {
				continue
			}

			elems := strings.Split(e.Message, " ")
			f(logInfo{Addr: elems[len(elems)-1]})
		}
	}()

	return w
}
