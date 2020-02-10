package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/lib/pq"

	"github.com/hblanks/sketches/2020-01-22-pgnotifier/ringbuffer"
)

const topic = "update"

func newListener(conninfo string) (*pq.Listener, error) {
	const minReconn = 1 * time.Second
	const maxReconn = 20 * time.Second

	reportProblem := func(ev pq.ListenerEventType, err error) {
		if err != nil {
			log.Printf("reportProblem: %v / %v / %v", ev, err, err.Error())
		}
	}

	listener := pq.NewListener(conninfo, minReconn, maxReconn, reportProblem)
	if err := listener.Listen(topic); err != nil {
		return nil, err
	}
	return listener, nil
}

// Sends NOTIFY events at (semi) regular intervals to the given DB.
func sendUpdates(conninfo string) {
	const updateFreq = time.Second

	db, err := sql.Open("postgres", conninfo)
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		log.Fatal(err)
	}

	i := 0
	for {
		_, err := conn.ExecContext(
			ctx,
			"SELECT pg_notify($1, $2)", topic, strconv.Itoa(i))
		if err != nil {
			log.Fatalf("sendUpdates: error %v", err)
		}
		time.Sleep(updateFreq)
		i++
		if i%10 == 0 {
			time.Sleep(30 * time.Second)
		}
	}
}

func waitItem(l *pq.Listener) (*ringbuffer.Item, error) {
	const retryWait = 10 * time.Second

	select {
	case v := <-l.Notify:
		// log.Printf("received notification: %#v", v)
		i, err := strconv.ParseInt(v.Extra, 10, 64)
		if err != nil {
			return nil, err
		}
		return &ringbuffer.Item{i}, nil

	case <-time.After(retryWait):
		go l.Ping() // make sure connection is still alive
		return nil, nil
	}
}

func WriteItems(listener *pq.Listener, rb *ringbuffer.RingBuffer) {
	var lastId int64 = -1
	for {
		item, err := waitItem(listener)
		switch {
		case err != nil:
			log.Fatalf("listen error: %v", err)

		case item == nil:
			continue

		case item.Id == lastId+1:
			rb.Write(item)

		default:
			// We've missed notifications, or we're starting fresh. Clean
			// out the buffer.
			log.Printf("WriteItems: lastId=%d => resetting buffer to start=%d",
				lastId, item.Id)
			rb.Reset(item.Id)
			rb.Write(item)
		}
		lastId = item.Id
	}
}

type RequestIdKey string

var requestIdKey RequestIdKey = "requestid"

func GetUpdates(ctx context.Context, rb *ringbuffer.RingBuffer, start int64, tick <-chan time.Time) error {
	requestId := ctx.Value(requestIdKey)
	for {
		var items []*ringbuffer.Item
		var err error

		now := time.Now()
		start, items, err = rb.Read(ctx, start)
		if err != nil {
			log.Printf("GetUpdates[%s]: error %v", requestId, err)
			return err
		}

		var ids []string
		for _, item := range items {
			ids = append(ids, strconv.FormatInt(item.Id, 10))
		}
		log.Printf("GetUpdates[%s]: item time=%s ids=%s",
			requestId, time.Since(now), strings.Join(ids, ","))

		// Wait before trying to fetch again
		select {
		case <-tick:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func main() {
	const bufferSize = 10
	const conninfo = "postgresql://postgres:development@127.0.0.1/sketch?sslmode=disable"

	listener, err := newListener(conninfo + "&fallback_application_name=listener")
	if err != nil {
		log.Fatalf("newListener failed: %v", err)
	}
	log.Printf("listener cap: %d", cap(listener.Notify)) // 32

	rb := ringbuffer.NewRingBuffer(bufferSize, 0)
	go WriteItems(listener, rb)

	type request struct {
		start     int64
		wait      time.Duration
		frequency time.Duration
	}
	reqs := []request{
		{0, 0, 2 * time.Second},
		{0, 0, 10 * time.Millisecond},
		{0, 1 * time.Second, 5 * time.Second},
		{9, 14 * time.Second, 5 * time.Second},

		// Will fail because it's too far behind.
		{3, 14 * time.Second, 5 * time.Second},
	}

	for _, req := range reqs {
		go func(req request) {
			requestId := fmt.Sprintf("start=%d,wait=%s,freq=%s",
				req.start, req.wait, req.frequency)
			ctx := context.WithValue(
				context.Background(), requestIdKey, requestId)
			time.Sleep(req.wait)
			GetUpdates(ctx, rb, req.start, time.Tick(req.frequency))
		}(req)
	}

	sendUpdates(conninfo + "&fallback_application_name=notifier")
}
