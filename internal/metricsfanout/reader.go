package metricsfanout

import (
	"errors"
	"time"
)

func (c *client) readLoop() {
	state := fragments{}
	for {
		if err := c.conn.SetReadDeadline(time.Now().Add(c.service.timing.read)); err != nil {
			c.terminate(ReasonPeer)
			return
		}
		value, err := readFrame(c.reader)
		if err != nil {
			c.readFailure(err)
			return
		}
		if !c.frameBudget.take(c.service.clockNow(), frameBurst, framesPerSecond) {
			c.requestClose(1008, nil, ReasonFrameRate)
			return
		}
		switch value.opcode {
		case 8:
			if !validClose(value.payload) {
				c.requestClose(1002, nil, ReasonProtocol)
			} else {
				c.peerOnce.Do(func() { close(c.peerClosed) })
				c.requestClose(1000, value.payload, ReasonPeer)
			}
			return
		case 9:
			select {
			case c.controls <- control{opcode: 10, payload: value.payload}:
			case <-c.done:
				return
			default:
				c.requestClose(1008, nil, ReasonFrameRate)
				return
			}
		case 10:
		default:
			if err := state.accept(value); err != nil {
				c.readFailure(err)
				return
			}
		}
	}
}

func (c *client) readFailure(err error) {
	if errors.Is(err, errFrameTooLarge) {
		c.requestClose(1009, nil, ReasonOversize)
		return
	}
	if errors.Is(err, errProtocol) {
		c.requestClose(1002, nil, ReasonProtocol)
		return
	}
	c.terminate(networkReason(err, ReasonPeer))
}
