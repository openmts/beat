package metricsfanout

import (
	"encoding/binary"
	"errors"
	"io"
	"unicode/utf8"
)

var errProtocol = errors.New("invalid websocket frame")

var errFrameTooLarge = errors.New("websocket frame exceeds limit")

type frame struct {
	opcode  byte
	final   bool
	payload []byte
}

func readFrame(reader io.Reader) (frame, error) {
	var header [2]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return frame{}, err
	}
	value := frame{opcode: header[0] & 15, final: header[0]&128 != 0}
	if !validFrameHeader(header) {
		return frame{}, errProtocol
	}
	size, err := frameLength(reader, header[1]&127)
	if err != nil {
		return frame{}, err
	}
	if err := validateFrameSize(value, size); err != nil {
		return frame{}, err
	}
	var mask [4]byte
	if _, err := io.ReadFull(reader, mask[:]); err != nil {
		return frame{}, err
	}
	value.payload = make([]byte, int(size))
	if _, err := io.ReadFull(reader, value.payload); err != nil {
		return frame{}, err
	}
	for index := range value.payload {
		value.payload[index] ^= mask[index%len(mask)]
	}
	return value, nil
}

func frameLength(reader io.Reader, indicator byte) (uint64, error) {
	if indicator < 126 {
		return uint64(indicator), nil
	}
	if indicator == 126 {
		var data [2]byte
		if _, err := io.ReadFull(reader, data[:]); err != nil {
			return 0, err
		}
		value := uint64(binary.BigEndian.Uint16(data[:]))
		if value < 126 {
			return 0, errProtocol
		}
		return value, nil
	}
	var data [8]byte
	if _, err := io.ReadFull(reader, data[:]); err != nil {
		return 0, err
	}
	value := binary.BigEndian.Uint64(data[:])
	if value <= 65535 || value>>63 != 0 {
		return 0, errProtocol
	}
	return value, nil
}

func validOpcode(value byte) bool {
	return value == 0 || value == 1 || value == 2 || value == 8 || value == 9 || value == 10
}

func serverFrame(opcode byte, payload []byte) []byte {
	header := 2
	if len(payload) >= 126 {
		header = 4
	}
	if len(payload) > 65535 {
		header = 10
	}
	output := make([]byte, header+len(payload))
	output[0] = 128 | opcode
	switch header {
	case 2:
		output[1] = byte(len(payload))
	case 4:
		output[1] = 126
		binary.BigEndian.PutUint16(output[2:4], uint16(len(payload)))
	case 10:
		output[1] = 127
		binary.BigEndian.PutUint64(output[2:10], uint64(len(payload)))
	}
	copy(output[header:], payload)
	return output
}

func validClose(payload []byte) bool {
	if len(payload) == 0 {
		return true
	}
	if len(payload) == 1 {
		return false
	}
	code := binary.BigEndian.Uint16(payload[:2])
	valid := code >= 3000 && code <= 4999 || code >= 1000 && code <= 1014 && code != 1004 && code != 1005 && code != 1006
	return valid && utf8.Valid(payload[2:])
}

type fragments struct {
	opcode byte
	data   []byte
}

func (state *fragments) accept(value frame) error {
	if value.opcode == 0 {
		if state.opcode == 0 {
			return errProtocol
		}
	} else {
		if state.opcode != 0 {
			return errProtocol
		}
		state.opcode = value.opcode
	}
	if len(state.data)+len(value.payload) > MaximumInboundBytes {
		return errFrameTooLarge
	}
	state.data = append(state.data, value.payload...)
	if !value.final {
		return nil
	}
	if state.opcode == 1 && !utf8.Valid(state.data) {
		return errProtocol
	}
	state.opcode = 0
	state.data = nil
	return nil
}

func validFrameHeader(header [2]byte) bool {
	return header[0]&112 == 0 && header[1]&128 != 0 && validOpcode(header[0]&15)
}

func validateFrameSize(value frame, size uint64) error {
	if value.opcode >= 8 && (!value.final || size > maximumControlBytes) {
		return errProtocol
	}
	if size > MaximumInboundBytes {
		return errFrameTooLarge
	}
	return nil
}
