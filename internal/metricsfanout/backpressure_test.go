package metricsfanout

import (
	"bufio"
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

func TestPendingQueueCoalescesWithoutBlockingProducer(t *testing.T) {
	s, clock := fixture(t, nil)
	c := &client{pending: make(chan *generation, 1)}
	if !s.attach(c) {
		t.Fatal("attach")
	}
	for range 10 {
		publishFixture(t, s)
		clock.advance(Cadence)
	}
	if len(c.pending) != 1 || s.Stats().Dropped != 9 {
		t.Fatal("unbounded pending generations", len(c.pending), s.Stats())
	}
	latest := <-c.pending
	if latest.number != 10 {
		t.Fatal("stale pending generation retained", latest.number)
	}
	s.detach(c)
}

func TestSlowWriterTimesOutAndProducerContinues(t *testing.T) {
	s, clock := fixture(t, nil)
	s.timing.write = 10 * time.Millisecond
	server, peer := net.Pipe()
	defer func() { _ = peer.Close() }()
	c := newClient(s, server, bufio.NewReader(server))
	c.admitted.Store(true)
	s.attach(c)
	finished := make(chan struct{})
	go func() { c.run(); s.detach(c); close(finished) }()
	publishFixture(t, s)
	clock.advance(Cadence)
	publishFixture(t, s)
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("slow writer did not terminate")
	}
	if s.Stats().Generation.Generation != 2 {
		t.Fatal("slow client blocked producer")
	}
}

func TestStaleQueuedGenerationNeverWritesAfterWindow(t *testing.T) {
	s, clock := fixture(t, nil)
	publishFixture(t, s)
	s.mu.Lock()
	generation := s.latest
	s.mu.Unlock()
	conn := &faultConn{}
	c := newClient(s, conn, bufio.NewReader(conn))
	clock.advance(StaleWindow + time.Second)
	if !c.writeGeneration(generation) || conn.writes.Load() != 0 {
		t.Fatal("expired payload written")
	}
	if s.Stats().Dropped != 1 {
		t.Fatal("stale drop not counted")
	}
}

type faultConn struct {
	deadlineErr, readErr, writeErr error
	zero                           bool
	writes                         atomic.Int64
}

func (c *faultConn) Read([]byte) (int, error) {
	if c.readErr != nil {
		return 0, c.readErr
	}
	return 0, errors.New("closed")
}

func (c *faultConn) Write(p []byte) (int, error) {
	c.writes.Add(1)
	if c.writeErr != nil {
		return 0, c.writeErr
	}
	if c.zero {
		return 0, nil
	}
	return len(p), nil
}

func (*faultConn) Close() error { return nil }

func (*faultConn) LocalAddr() net.Addr { return &net.TCPAddr{} }

func (*faultConn) RemoteAddr() net.Addr { return &net.TCPAddr{} }

func (c *faultConn) SetDeadline(time.Time) error { return c.deadlineErr }

func (c *faultConn) SetReadDeadline(time.Time) error { return c.deadlineErr }

func (c *faultConn) SetWriteDeadline(time.Time) error { return c.deadlineErr }

type timeoutError struct{}

func (timeoutError) Error() string { return "private timeout" }

func (timeoutError) Timeout() bool { return true }

func (timeoutError) Temporary() bool { return true }

func TestWriteAndReadFailuresCloseExactlyOnce(t *testing.T) {
	s, _ := fixture(t, nil)
	for _, conn := range []*faultConn{{deadlineErr: errors.New("deadline")}, {writeErr: timeoutError{}}, {zero: true}} {
		c := newClient(s, conn, bufio.NewReader(conn))
		if c.send([]byte("payload"), time.Second) {
			t.Fatal("failed write accepted")
		}
		c.terminate(ReasonPeer)
		select {
		case <-c.done:
		default:
			t.Fatal("connection not closed")
		}
	}
	c := newClient(s, &faultConn{deadlineErr: errors.New("deadline")}, bufio.NewReader(&faultConn{}))
	c.readLoop()
	select {
	case <-c.done:
	default:
		t.Fatal("read deadline failure not closed")
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if err := s.Run(canceled); err != nil {
		t.Fatal(err)
	}
	if s.Stats().Builds != 0 {
		t.Fatal("canceled startup built")
	}
}
