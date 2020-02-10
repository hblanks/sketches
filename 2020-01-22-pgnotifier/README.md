# Sketch for consuming updates from a postgres DB (2020-01-22)

How to run it:

```
docker-compose up -d
go run pgnotifier.go 2&>1 | tee /tmp/update.log
```

## Looking at logs

To get a view of what each request goroutine is doing, group by
requestid:

```
sort -k 3,3 -k 1,2 /tmp/update.log  | less
```

## Next steps understood at the time

This code expects that NOTIFY events contain all necessary data.

But, in practice, NOTIFY events will just be an identifier of the latest
offset id written into a log table. So, the routine that LISTENS for
these events will need to SELECT from the log table, and indeed insert
these rows, instead of just ids, into the ring buffer.

This also implies RingBuffer' write call should take a slice of items,
not just one.

## Things only learned later on

Although the RingBuffer in this sketch works for the cases given, it's
breaks in edge cases of consumers coming and going and falling behind,
and it works poorly when the ring buffer is started up again after some
events are present in the log table.

In particular, the `Read()` function required a fairly extensive
rewrite, and also became more domain specific (callers are able to
filter events by the agent ID of the `*pb.AgentEvent` structs in the
buffer).

The end result, as of 2020-02-09, was:

```go
// Reads all matching items from the ring buffer. Returns:
//
//	1. The next offset the caller should read from.
//	2. All matching events.
//	3. Any error.
//
// Blocking behavior and return values, given:
//
//	- start S,
//	- initial buffer range [A, B), and
//	- range after blocking (if any) [C, D)
//
//	Case					Return values
//	---------------------  	------------------
//					(non-blocking)
//	1. S < A				S, ErrBeforeBuffer
//	2. S < B				B, [S, B)
//					(blocking)
//	3. S >= B && S < C		S, ErrBeforeBuffer
//	4. S >= B && S < D		D, [S, D)
//	5. S >= B && S == D		D, [D, D)
//	6. S >= B && S > D		S, ErrAfterBuffer
//
// The offset value returned is only ever the same or higher than start.
func (rb *RingBuffer) Read(ctx context.Context, agentId, start int64) (int64, []*pb.AgentEvent, error) {
	// Lock for all of this function except Wait(), so that Write() is
	// not able to advance rb.start out from under us.
	rb.cond.L.Lock()
	defer rb.cond.L.Unlock()

	switch {
	case start < rb.start:
		return rb.start, nil, fmt.Errorf("%w: (start=%d rb.start=%d distance=%d)",
			ErrBeforeBuffer, start, rb.start, rb.start-start)
	case start < rb.end:
		// Don't wait for new records. We already have ones to process.
	case start >= rb.end:
		// No new records to process just yet. Wait()
		rb.cond.Wait()
		if ctx.Err() != nil {
			// Bail if we timed out/cancelled during wait
			return start, nil, ctx.Err()
		}
		switch {
		case start < rb.start:
			return rb.start, nil, fmt.Errorf("%w: (start=%d rb.start=%d distance=%d)",
				ErrBeforeBuffer, start, rb.start, rb.start-start)
		case start < rb.end:
			// We have records. Process them.
		case start == rb.end:
			return start, nil, nil
		case start > rb.end:
			return start, nil, ErrAfterBuffer
		}
	}

	// log.Printf("Read(): start=%d rb.start=%d rb.end=%d", start, rb.start, rb.end)
	items := make([]*pb.AgentEvent, 0, rb.end-start)
	for ; start < rb.end; start++ {
		bufOffset := start % rb.size
		if agentId == 0 || rb.buffer[bufOffset].AgentId == agentId {
			items = append(items, rb.buffer[bufOffset])
		}
	}
	return start, items, nil
}
```
