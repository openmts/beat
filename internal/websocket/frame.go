package websocket

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
)

const frameShortPayload = 125

const frameMediumMarker = 126

const frameLargeMarker = 127

const shortFrameHeaderBytes = 2

const mediumFrameHeaderBytes = 4

const largeFrameHeaderBytes = 10

const finalTextFrame = 0x81

func encodeTextFrame(payload []byte) ([]byte, error) {
	if len(payload) > maxFramePayload {
		return nil, errors.New("websocket frame exceeds payload limit")
	}
	headerSize := shortFrameHeaderBytes
	if len(payload) > math.MaxUint16 {
		headerSize = largeFrameHeaderBytes
	} else if len(payload) > frameShortPayload {
		headerSize = mediumFrameHeaderBytes
	}
	frame := make([]byte, headerSize+len(payload))
	frame[0] = finalTextFrame
	switch headerSize {
	case shortFrameHeaderBytes:
		frame[1] = byte(len(payload))
	case mediumFrameHeaderBytes:
		frame[1] = frameMediumMarker
		binary.BigEndian.PutUint16(frame[shortFrameHeaderBytes:mediumFrameHeaderBytes], uint16(len(payload)))
	case largeFrameHeaderBytes:
		frame[1] = frameLargeMarker
		binary.BigEndian.PutUint64(frame[shortFrameHeaderBytes:largeFrameHeaderBytes], uint64(len(payload)))
	}
	copy(frame[headerSize:], payload)
	return frame, nil
}

func writeWSFrame(conn net.Conn, payload []byte) error {
	frame, err := encodeTextFrame(payload)
	if err != nil {
		return err
	}
	for len(frame) > 0 {
		written, err := conn.Write(frame)
		if err != nil {
			return fmt.Errorf("write frame: %w", err)
		}
		if written == 0 {
			return io.ErrShortWrite
		}
		frame = frame[written:]
	}
	return nil
}
