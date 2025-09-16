package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/go-faster/errors"
	"github.com/google/uuid"
	"log/slog"

	"github.com/ClickHouse/ch-go"
	"github.com/ClickHouse/ch-go/proto"
)

func run(ctx context.Context) error {
	lg := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	c, err := ch.Dial(ctx, ch.Options{Logger: lg})
	if err != nil {
		return errors.Wrap(err, "dial")
	}
	defer func() {
		_ = c.Close()
	}()

	type Report struct {
		Type   proto.ColumnType
		Insert time.Duration
		Select time.Duration
	}

	var reports []Report

	const n = 500000
	for _, v := range []func() (
		col proto.Column,
	){
		func() (col proto.Column) {
			d := make(proto.ColUInt128, n)
			for i := range d {
				d[i] = proto.UInt128{Low: 1}
			}
			return &d
		},
		func() (col proto.Column) {
			d := make(proto.ColUUID, n)
			for i := range d {
				d[i] = uuid.UUID{1, 2, 10: 3}
			}
			return &d
		},
		func() (col proto.Column) {
			d := make(proto.ColUInt64, n)
			for i := range d {
				d[i] = uint64(i)
			}
			return &d
		},
		func() proto.Column {
			const size = 16
			d := proto.ColFixedStr{
				Buf:  make([]byte, size*n),
				Size: size,
			}
			for i := 0; i < n; i++ {
				binary.BigEndian.PutUint64(d.Buf[i*size:], uint64(i))
			}
			return &d
		},
		func() proto.Column {
			const size = 8
			d := proto.ColFixedStr{
				Buf:  make([]byte, size*n),
				Size: size,
			}
			for i := 0; i < n; i++ {
				binary.BigEndian.PutUint64(d.Buf[i*size:], uint64(i))
			}
			return &d
		},
	} {
		if err := c.Do(ctx, ch.Query{
			Body: "DROP TABLE bench_trace_id",
		}); err != nil && !ch.IsErr(err, proto.ErrUnknownTable) {
			return errors.Wrap(err, "create table")
		}

		data := v()
		ddl := fmt.Sprintf("CREATE TABLE bench_trace_id (trace_id %s) ENGINE=Memory", data.Type())
		if err := c.Do(ctx, ch.Query{
			Body: ddl,
		}); err != nil {
			return errors.Wrap(err, "create")
		}

		report := Report{
			Type: data.Type(),
		}

		lg := lg.With(
			"t", data.Type().String(),
		)

		const targetRows = n * 100
		var rows int
		start := time.Now()
		if err := c.Do(ctx, ch.Query{
			Body: "INSERT INTO bench_trace_id VALUES",
			Settings: []ch.Setting{
				{
					Key:       "send_logs_level",
					Value:     "trace",
					Important: true,
				},
			},
			OnLog: func(ctx context.Context, l ch.Log) error {
				switch l.Source {
				case "MemoryTracker": // ok
				default:
					return nil // skip
				}
				lg.Info("Log",
					"source", l.Source,
					"text", l.Text,
				)
				return nil
			},
			OnInput: func(ctx context.Context) error {
				lg.Debug("Fetching", "type", data.Type().String())
				rows += n
				if rows >= targetRows {
					return io.EOF
				}
				return nil
			},
			Input: []proto.InputColumn{
				{Name: "trace_id", Data: data},
			},
		}); err != nil {
			return errors.Wrap(err, "query")
		}

		report.Insert = time.Since(start)
		lg.Info("Done", "duration", report.Insert)

		start = time.Now()
		if err := c.Do(ctx, ch.Query{
			Body: "SELECT trace_id FROM bench_trace_id",
			Settings: []ch.Setting{
				{
					Key:       "send_logs_level",
					Value:     "trace",
					Important: true,
				},
			},
			OnResult: func(ctx context.Context, block proto.Block) error {
				// Skip.
				return nil
			},
			OnLog: func(ctx context.Context, l ch.Log) error {
				switch l.Source {
				case "MemoryTracker", "executeQuery": // ok
				default:
					return nil // skip
				}
				lg.Info("Log",
					"source", l.Source,
					"text", l.Text,
				)
				return nil
			},
			Result: proto.Results{
				{Name: "trace_id", Data: data},
			},
		}); err != nil {
			return errors.Wrap(err, "query")
		}

		report.Select = time.Since(start)
		lg.Info("Select", "duration", report.Select)

		reports = append(reports, report)
	}

	const format = "%16s | %10s | %10s\n"
	fmt.Printf(format, "type", "insert", "select")
	for _, report := range reports {
		fmt.Printf(format,
			report.Type,
			report.Insert.Round(time.Millisecond).String(),
			report.Select.Round(time.Millisecond).String(),
		)
	}

	return nil
}

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %+v\n", err)
		os.Exit(2)
	}
}
