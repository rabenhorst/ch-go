package ch

import (
	"context"
	"errors"
	"net"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/ClickHouse/ch-go/cht"
	"github.com/ClickHouse/ch-go/internal/gold"
	"github.com/ClickHouse/ch-go/proto"
)

func TestMain(m *testing.M) {
	// Explicitly registering flags for golden files.
	gold.Init()

	os.Exit(m.Run())
}

func ConnOpt(t testing.TB, opt Options) *Client {
	t.Helper()

	ctx := context.Background()
	server := cht.New(t)

	if opt.Logger == nil {
		opt.Logger = zaptest.NewLogger(t)
	}

	opt.Address = server.TCP
	client, err := Dial(ctx, opt)
	require.NoError(t, err)

	t.Log("Connected", client.ServerInfo())
	t.Cleanup(func() {
		require.NoError(t, client.Close())
	})

	return client
}

func Conn(t testing.TB) *Client {
	return ConnOpt(t, Options{})
}

func SkipNoFeature(t *testing.T, client *Client, feature proto.Feature) {
	if !client.ServerInfo().Has(feature) {
		t.Skipf("Skipping (feature %q not supported)", feature)
	}
}

func TestDial(t *testing.T) {
	t.Run("Ok", func(t *testing.T) {
		conn := Conn(t)
		require.NoError(t, conn.Ping(context.Background()))
	})
	t.Run("Closed", func(t *testing.T) {
		ctx := context.Background()
		server := cht.New(t)
		conn, err := Dial(ctx, Options{
			Address: server.TCP,
		})
		require.NoError(t, err)
		require.NoError(t, conn.Ping(ctx))
		require.NoError(t, conn.Close())
		require.ErrorIs(t, conn.Ping(ctx), ErrClosed)
		require.ErrorIs(t, conn.Do(ctx, Query{}), ErrClosed)
	})
	t.Run("DatabaseNotFound", func(t *testing.T) {
		ctx := context.Background()
		server := cht.New(t)
		client, err := Dial(ctx, Options{
			Address:  server.TCP,
			Database: "bad",
		})
		if IsErr(err, proto.ErrUnknownDatabase) {
			t.Skip("got error during handshake")
		}
		require.NoError(t, err)
		err = client.Do(ctx, Query{
			Body:   "SELECT 1",
			Result: discardResult(),
		})
		require.True(t, IsErr(err, proto.ErrUnknownDatabase))
	})
}

func TestExceptionUnwrap(t *testing.T) {
	flat := &Exception{
		Code:    proto.ErrReadonly,
		Name:    "foo",
		Message: "bar",
		Next:    nil,
	}

	if !errors.Is(flat, proto.ErrReadonly) {
		t.Fatal("flat exception must be the error code it represents")
	}

	nested := &Exception{
		Code:    proto.ErrAborted,
		Name:    "foo",
		Message: "bar",
		Next:    []Exception{*flat},
	}
	if !errors.Is(nested, proto.ErrAborted) {
		t.Fatal("nested exception must be the error code it represents")
	}
	if !errors.Is(nested, proto.ErrReadonly) {
		t.Fatal("nested exception must be the error code it wraps")
	}
}

func TestBufferPool(t *testing.T) {
	// Create a mock connection
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	// Create multiple clients to test buffer reuse
	const numClients = 5

	// Create clients
	for i := 0; i < numClients; i++ {
		clientConn, serverConn := net.Pipe()
		defer clientConn.Close()
		defer serverConn.Close()

		// Start a goroutine to handle the server side of the handshake
		go func() {
			// Just close the connection after a short delay to simulate handshake failure
			time.Sleep(10 * time.Millisecond)
			serverConn.Close()
		}()

		// This will fail during handshake, but that's ok for testing buffer pool
		_, err := Connect(context.Background(), clientConn, Options{
			HandshakeTimeout: 50 * time.Millisecond,
		})
		require.Error(t, err) // Expect handshake to fail
	}

	// Check that pool stats show activity
	finalStats := BufferPoolStats()
	require.NotNil(t, finalStats)

	// The pool should have been accessed
	require.True(t, finalStats.AcquireCount() > 0, "Buffer pool should have been used")

	// All buffers should be returned to the pool (no leaks)
	require.Equal(t, int32(0), finalStats.AcquiredResources(), "All buffers should be returned to pool")
}

func TestBufferPoolStats(t *testing.T) {
	stats := BufferPoolStats()
	require.NotNil(t, stats)

	// Should have reasonable defaults
	require.True(t, stats.MaxResources() > 0)
}
