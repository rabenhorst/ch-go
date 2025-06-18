package proto

import (
	"io"
	"net"
	"sync"
)

// bufferPool is a global pool for reusing Buffer instances
var bufferPool = sync.Pool{
	New: func() interface{} {
		return &Buffer{}
	},
}

// getBuffer gets a buffer from the pool
func getBuffer() *Buffer {
	return bufferPool.Get().(*Buffer)
}

// putBuffer returns a buffer to the pool after resetting it
func putBuffer(buf *Buffer) {
	buf.Reset()
	bufferPool.Put(buf)
}

// Writer is a column writer.
//
// It helps to reduce memory footprint by writing column using vector I/O.
type Writer struct {
	conn io.Writer

	buf       *Buffer
	bufOffset int
	needCut   bool
	ownsBuf   bool // true if buf was obtained from pool and should be returned

	vec net.Buffers
}

// NewWriter creates new [Writer] with an externally provided buffer.
// Use this when you want to manage the buffer lifecycle yourself.
func NewWriter(conn io.Writer, buf *Buffer) *Writer {
	w := &Writer{
		conn:    conn,
		buf:     buf,
		ownsBuf: false, // externally provided buffer
		vec:     make(net.Buffers, 0, 16),
	}
	return w
}

// NewWriterWithPool creates new [Writer] using a buffer from the pool.
// The buffer will be automatically returned to the pool when the Writer is done.
func NewWriterWithPool(conn io.Writer) *Writer {
	buf := getBuffer()
	w := &Writer{
		conn:    conn,
		buf:     buf,
		ownsBuf: true, // we own this buffer and should return it to pool
		vec:     make(net.Buffers, 0, 16),
	}
	return w
}

// ChainWrite adds buffer to the vector to write later.
//
// Passed byte slice may be captured until [Writer.Flush] is called.
func (w *Writer) ChainWrite(data []byte) {
	w.cutBuffer()
	w.vec = append(w.vec, data)
}

// ChainBuffer creates a temporary buffer and adds it to the vector to write later.
//
// Data is not written immediately, call [Writer.Flush] to flush data.
//
// NB: do not retain buffer.
func (w *Writer) ChainBuffer(cb func(*Buffer)) {
	cb(w.buf)
}

func (w *Writer) cutBuffer() {
	newOffset := len(w.buf.Buf)
	data := w.buf.Buf[w.bufOffset:newOffset:newOffset]
	if len(data) == 0 {
		return
	}
	w.bufOffset = newOffset
	w.vec = append(w.vec, data)
}

func (w *Writer) reset() {
	w.bufOffset = 0
	w.needCut = false
	w.buf.Reset()
	// Do not hold references, to avoid memory leaks.
	clear(w.vec)
	w.vec = w.vec[:0]
}

// Flush flushes all data to writer.
func (w *Writer) Flush() (n int64, err error) {
	w.cutBuffer()
	n, err = w.vec.WriteTo(w.conn)
	w.reset()
	return n, err
}

// Close cleans up the Writer and returns any pooled buffer.
// After calling Close, the Writer should not be used anymore.
func (w *Writer) Close() {
	if w.ownsBuf && w.buf != nil {
		putBuffer(w.buf)
		w.buf = nil
		w.ownsBuf = false
	}
}
