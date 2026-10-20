package metricsfanout

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"
)

func maskedFrame(opcode byte, final bool, payload []byte) []byte {
	output := serverFrame(opcode, payload)
	if !final {
		output[0] &= 127
	}
	output[1] |= 128
	header := 2
	if output[1]&127 == 126 {
		header = 4
	}
	if output[1]&127 == 127 {
		header = 10
	}
	mask := []byte{1, 2, 3, 4}
	result := append([]byte(nil), output[:header]...)
	result = append(result, mask...)
	for i, value := range payload {
		result = append(result, value^mask[i%len(mask)])
	}
	return result
}

func TestFrameCodecLengthsAndMasking(t *testing.T) {
	for _, size := range []int{0, 1, 125, 126, MaximumInboundBytes} {
		payload := bytes.Repeat([]byte("x"), size)
		value, err := readFrame(bytes.NewReader(maskedFrame(1, true, payload)))
		if err != nil || !value.final || value.opcode != 1 || !bytes.Equal(value.payload, payload) {
			t.Fatal(size, err)
		}
	}
	for _, size := range []int{125, 126, 65535, 65536} {
		encoded := serverFrame(1, make([]byte, size))
		reader := bytes.NewReader(encoded[2:])
		length, err := frameLength(reader, encoded[1])
		if err != nil || length != uint64(size) || encoded[0] != 129 || encoded[1]&128 != 0 {
			t.Fatal(size, length, err)
		}
	}
}

func TestFrameDecoderRejectsMalformedControlAndLengthFields(t *testing.T) {
	cases := [][]byte{
		{}, {129}, {129, 0}, {193, 128}, {131, 128},
		maskedFrame(9, false, []byte("ping")), maskedFrame(9, true, make([]byte, 126)),
		{129, 254, 0, 1}, {129, 255, 0, 0, 0, 0, 0, 0, 0, 126}, {129, 255, 128, 0, 0, 0, 0, 0, 0, 0},
		{129, 254, 0}, {129, 255, 0}, maskedFrame(1, true, make([]byte, MaximumInboundBytes+1)),
		{129, 129, 1, 2}, {129, 129, 1, 2, 3, 4},
	}
	for _, raw := range cases {
		if _, err := readFrame(bytes.NewReader(raw)); err == nil {
			t.Fatal("invalid frame accepted", raw[:min(len(raw), 12)])
		}
	}
}

func TestFragmentationAndUTF8(t *testing.T) {
	state := fragments{}
	if err := state.accept(frame{opcode: 0, final: true}); err != errProtocol {
		t.Fatal(err)
	}
	if err := state.accept(frame{opcode: 1, payload: []byte{0xe4}}); err != nil {
		t.Fatal(err)
	}
	if err := state.accept(frame{opcode: 1, final: true}); err != errProtocol {
		t.Fatal(err)
	}
	if err := state.accept(frame{opcode: 0, final: true, payload: []byte{0xbd, 0xa0}}); err != nil {
		t.Fatal(err)
	}
	if err := state.accept(frame{opcode: 1, final: true, payload: []byte{0xff}}); err != errProtocol {
		t.Fatal(err)
	}
	state = fragments{}
	if err := state.accept(frame{opcode: 2, payload: make([]byte, MaximumInboundBytes)}); err != nil {
		t.Fatal(err)
	}
	if err := state.accept(frame{opcode: 0, final: true, payload: []byte{0}}); err != errFrameTooLarge {
		t.Fatal(err)
	}
}

func TestCloseCodeAndReasonValidation(t *testing.T) {
	if !validClose(nil) || validClose([]byte{0}) {
		t.Fatal("empty/partial close")
	}
	for _, code := range []uint16{1000, 1001, 1002, 1003, 1007, 1008, 1009, 1010, 1011, 1012, 1013, 1014, 3000, 4999} {
		payload := make([]byte, 2)
		binary.BigEndian.PutUint16(payload, code)
		if !validClose(payload) {
			t.Fatal(code)
		}
	}
	for _, code := range []uint16{0, 999, 1004, 1005, 1006, 1015, 2000, 5000} {
		payload := make([]byte, 2)
		binary.BigEndian.PutUint16(payload, code)
		if validClose(payload) {
			t.Fatal(code)
		}
	}
	if validClose([]byte{3, 232, 0xff}) {
		t.Fatal("invalid close UTF8")
	}
	if _, err := frameLength(bytes.NewReader(nil), 126); err != io.EOF {
		t.Fatal(err)
	}
}
