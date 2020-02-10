package ringbuffer

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

type Item struct {
	Id int64
}

// A RingBuffer provides concurrent write and read access to a fixed
// number of items. Writers write items to a monotonically increasing
// virtual offset in the buffer. Readers can read items from within
// the buffer or wait for new items to appear.
//
// This RingBuffer facilitates dispatch events from a single stream to
// multiple active consumers.
type RingBuffer struct {
	rw     *sync.RWMutex
	cond   *sync.Cond
	buffer []*Item
	size   int64 // Length of buffer
	start  int64 // Start offset within ring buffer
	end    int64 // End offset in ring buffer (exclusive).
}

// Creates a buffer of a given size and with a given offset.
func NewRingBuffer(size int, start int64) *RingBuffer {
	rw := &sync.RWMutex{}
	cond := sync.NewCond(rw.RLocker())
	return &RingBuffer{
		rw:     rw,
		cond:   cond,
		buffer: make([]*Item, size),
		size:   int64(size),
		start:  start,
		end:    start,
	}
}

// Writes an item to the ring buffer and notifies all waiting readers.
func (rb *RingBuffer) Write(item *Item) {
	rb.rw.Lock()
	bufOffset := rb.end % rb.size
	rb.buffer[bufOffset] = item
	if rb.end-rb.start == rb.size {
		rb.start++ // advance start if we've exceeded size
	}
	rb.end++
	rb.rw.Unlock()
	rb.cond.Broadcast()
}

// Resets the buffer to a new offset and releases all waiting readers.
func (rb *RingBuffer) Reset(start int64) {
	rb.cond.L.Lock()
	rb.start = start
	rb.end = start
	rb.cond.L.Unlock()
	rb.cond.Broadcast()
}

var ErrAfterBuffer = errors.New("Requested start is ahead of buffer")
var ErrBeforeBuffer = errors.New("Requested start is behind start of buffer")

// Return the end offset to read up to in our buffer, given a start
// offset.
func (rb *RingBuffer) endOffset(start int64) (int64, error) {
	switch {
	case start < rb.start:
		return -1, fmt.Errorf("%w: (start=%d rb.start=%d distance=%d)",
			ErrBeforeBuffer, start, rb.start, rb.start-start)

	case start < rb.end:
		// Don't wait for new records. We already have ones to process.
		return rb.end, nil

	case start == rb.end:
		// No new records to process just yet. Wait.
		rb.cond.Wait()
		return rb.end, nil

	case start >= rb.end:
		return -1, ErrAfterBuffer

	default:
		panic("missing case") // should not happen
	}
}

// Reads any pending items from the ring buffer, returning the next
// offset to read.
func (rb *RingBuffer) Read(ctx context.Context, start int64) (int64, []*Item, error) {
	// Lock for the length of the function, so that Write() is not able
	// to advance rb.start out from under us.
	rb.cond.L.Lock()
	defer rb.cond.L.Unlock()
	end, err := rb.endOffset(start)
	if err != nil {
		return start, nil, err
	}
	// Check that we didn't timeout calls to Lock() / Wait() above.
	if ctx.Err() != nil {
		return start, nil, ctx.Err()
	}

	items := make([]*Item, 0, end-start)
	for ; start < end; start++ {
		bufOffset := start % rb.size
		items = append(items, rb.buffer[bufOffset])
	}
	return start, items, nil
}
