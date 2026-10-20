package metricsfanout

import (
	"bufio"
	"encoding/binary"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

type control struct {
	opcode  byte
	payload []byte
	reason  Reason
}

type client struct {
	service     *Service
	conn        net.Conn
	reader      *bufio.Reader
	pending     chan *generation
	controls    chan control
	closing     chan control
	done        chan struct{}
	peerClosed  chan struct{}
	peerOnce    sync.Once
	once        sync.Once
	frameBudget bucket
	admitted    atomic.Bool
}

func newClient(s *Service, conn net.Conn, reader *bufio.Reader) *client {
	return &client{service: s, conn: conn, reader: reader, pending: make(chan *generation, 1), controls: make(chan control, controlQueueCapacity), closing: make(chan control, 1), done: make(chan struct{}), peerClosed: make(chan struct{}), frameBudget: bucket{tokens: frameBurst, at: s.clockNow()}}
}

func (s *Service) attach(c *client) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing || s.ctx.Err() != nil {
		return false
	}
	s.clients[c] = struct{}{}
	if s.latest != nil && s.ageLocked(s.latest) <= StaleWindow {
		c.enqueue(s.latest)
	}
	return true
}

func (s *Service) detach(c *client) { s.mu.Lock(); delete(s.clients, c); s.mu.Unlock() }

func (c *client) enqueue(value *generation) bool {
	select {
	case c.pending <- value:
		return false
	default:
	}
	dropped := false
	select {
	case <-c.pending:
		dropped = true
	default:
	}
	select {
	case c.pending <- value:
	default:
	}
	return dropped
}

func (c *client) run() {
	var wait sync.WaitGroup
	wait.Add(2)
	go func() { defer wait.Done(); c.writeLoop() }()
	go func() { defer wait.Done(); c.readLoop() }()
	wait.Wait()
}

func (c *client) terminate(reason Reason) {
	c.once.Do(func() {
		close(c.done)
		_ = c.conn.Close()
		if c.admitted.Load() {
			c.service.emit(Event{Kind: EventConnection, Reason: reason, Count: -1})
		}
	})
}

func (c *client) requestClose(code uint16, payload []byte, reason Reason) {
	if payload == nil {
		payload = make([]byte, 2)
		binary.BigEndian.PutUint16(payload, code)
	}
	select {
	case c.closing <- control{opcode: 8, payload: payload, reason: reason}:
	default:
	}
}

func (c *client) writeLoop() {
	ticker := time.NewTicker(c.service.timing.ping)
	defer ticker.Stop()
	defer c.terminate(ReasonPeer)
	for {
		select {
		case closeFrame := <-c.closing:
			c.finishClose(closeFrame)
			return
		default:
		}
		select {
		case <-c.done:
			return
		case <-c.service.ctx.Done():
			c.finishClose(control{opcode: 8, payload: []byte{3, 233}, reason: ReasonClosed})
			return
		case closeFrame := <-c.closing:
			c.finishClose(closeFrame)
			return
		case message := <-c.controls:
			if !c.send(serverFrame(message.opcode, message.payload), c.service.timing.write) {
				return
			}
		case value := <-c.pending:
			if !c.writeGeneration(value) {
				return
			}
		case <-ticker.C:
			if !c.send(serverFrame(9, []byte("beat")), c.service.timing.write) {
				return
			}
		}
	}
}

func (c *client) writeGeneration(value *generation) bool {
	age := c.service.age(value)
	if age >= StaleWindow {
		c.service.mu.Lock()
		c.service.stats.Dropped++
		c.service.mu.Unlock()
		c.service.emit(Event{Kind: EventDrop, Reason: ReasonStale, Count: 1})
		return true
	}
	timeout := c.service.timing.write
	if remaining := StaleWindow - age; remaining < timeout {
		timeout = remaining
	}
	return c.send(value.frame, timeout)
}

func (c *client) finishClose(frame control) {
	if !c.send(serverFrame(8, frame.payload), c.service.timing.write) {
		return
	}
	if frame.reason == ReasonClosed {
		timer := time.NewTimer(c.service.timing.write)
		defer timer.Stop()
		select {
		case <-c.peerClosed:
		case <-c.done:
		case <-timer.C:
		}
	}
	c.terminate(frame.reason)
}

func (c *client) send(payload []byte, timeout time.Duration) bool {
	if err := c.conn.SetWriteDeadline(time.Now().Add(timeout)); err != nil {
		c.terminate(ReasonWrite)
		return false
	}
	for len(payload) > 0 {
		count, err := c.conn.Write(payload)
		if err != nil {
			c.terminate(networkReason(err, ReasonWrite))
			return false
		}
		if count <= 0 || count > len(payload) {
			c.terminate(ReasonWrite)
			return false
		}
		payload = payload[count:]
	}
	return true
}

func networkReason(err error, fallback Reason) Reason {
	if network, ok := err.(net.Error); ok && network.Timeout() {
		return ReasonTimeout
	}
	return fallback
}
