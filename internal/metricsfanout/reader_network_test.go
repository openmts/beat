package metricsfanout

import (
	"encoding/binary"
	"net/http/httptest"
	"testing"
	"time"
)

func TestInboundDataFailuresAndRateLimit(t *testing.T) {
	cases := []struct {
		name   string
		frames []byte
		code   uint16
	}{
		{"invalid close", maskedFrame(8, true, []byte{1}), 1002},
		{"unexpected continuation", maskedFrame(0, true, nil), 1002},
		{"oversize", maskedFrame(2, true, make([]byte, MaximumInboundBytes+1)), 1009},
	}
	flood := []byte{}
	for range frameBurst + 1 {
		flood = append(flood, maskedFrame(10, true, nil)...)
	}
	cases = append(cases, struct {
		name   string
		frames []byte
		code   uint16
	}{"rate", flood, 1008})
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := fixture(t, nil)
			s.timing.write = 20 * time.Millisecond
			server := httptest.NewServer(s)
			defer server.Close()
			conn, reader, _ := dialRaw(t, server.URL, "")
			if _, err := conn.Write(tc.frames); err != nil {
				t.Fatal(err)
			}
			opcode, payload := readServerFrame(t, reader)
			if opcode != 8 || binary.BigEndian.Uint16(payload) != tc.code {
				t.Fatal(opcode, payload)
			}
			await(t, func() bool { return s.Stats().Reserved == 0 })
		})
	}
}

func TestServerPingAndReadTimeout(t *testing.T) {
	s, _ := fixture(t, nil)
	s.timing.ping = 5 * time.Millisecond
	s.timing.read = 30 * time.Millisecond
	server := httptest.NewServer(s)
	defer server.Close()
	_, reader, _ := dialRaw(t, server.URL, "")
	opcode, payload := readServerFrame(t, reader)
	if opcode != 9 || string(payload) != "beat" {
		t.Fatal(opcode, payload)
	}
	await(t, func() bool { return s.Stats().Reserved == 0 })
}
