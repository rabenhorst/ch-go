package proto

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewWriterWithPool(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriterWithPool(&buf)
	require.NotNil(t, w)
	require.NotNil(t, w.buf)
	require.True(t, w.ownsBuf)

	// Test that we can use the writer
	w.ChainBuffer(func(b *Buffer) {
		b.PutString("test")
	})

	n, err := w.Flush()
	require.NoError(t, err)
	require.Greater(t, n, int64(0))

	// Test Close returns buffer to pool
	w.Close()
	require.Nil(t, w.buf)
	require.False(t, w.ownsBuf)

	// Test that the buffer was returned to pool by getting another one
	w2 := NewWriterWithPool(&bytes.Buffer{})
	require.NotNil(t, w2.buf)
	// The buffer should be reused (though this isn't guaranteed by sync.Pool)
	// but at least we can verify it's been reset
	require.Equal(t, 0, len(w2.buf.Buf))
	w2.Close()
}

func TestNewWriter(t *testing.T) {
	var buf bytes.Buffer
	customBuf := &Buffer{}
	w := NewWriter(&buf, customBuf)
	require.NotNil(t, w)
	require.Equal(t, customBuf, w.buf)
	require.False(t, w.ownsBuf)

	// Test that Close doesn't affect externally provided buffer
	w.Close()
	require.Equal(t, customBuf, w.buf)
	require.False(t, w.ownsBuf)
}

func TestBufferPool(t *testing.T) {
	// Test that buffers are properly reset when returned to pool
	buf1 := getBuffer()
	buf1.PutString("test data")
	require.Greater(t, len(buf1.Buf), 0)

	putBuffer(buf1)

	buf2 := getBuffer()
	require.Equal(t, 0, len(buf2.Buf), "buffer should be reset when returned to pool")

	putBuffer(buf2)
}

func TestWriterBufferPoolIntegration(t *testing.T) {
	var output bytes.Buffer

	// Create writer with pooled buffer
	w := NewWriterWithPool(&output)

	// Write some data
	w.ChainBuffer(func(b *Buffer) {
		b.PutString("Hello")
	})
	w.ChainBuffer(func(b *Buffer) {
		b.PutString("World")
	})

	// Flush data
	n, err := w.Flush()
	require.NoError(t, err)
	require.Greater(t, n, int64(0))

	// Verify data was written
	require.Greater(t, output.Len(), 0)

	// Clean up
	w.Close()
}

func BenchmarkWriterWithPool(b *testing.B) {
	var output bytes.Buffer

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		output.Reset()
		w := NewWriterWithPool(&output)
		w.ChainBuffer(func(buf *Buffer) {
			buf.PutString("benchmark test")
		})
		w.Flush()
		w.Close()
	}
}

func BenchmarkWriterWithoutPool(b *testing.B) {
	var output bytes.Buffer

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		output.Reset()
		w := NewWriter(&output, &Buffer{})
		w.ChainBuffer(func(buf *Buffer) {
			buf.PutString("benchmark test")
		})
		w.Flush()
	}
}
