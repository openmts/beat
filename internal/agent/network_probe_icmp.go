package agent

import (
	"context"
	"net"
	"os"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"

	"github.com/beat/backend/internal/model"
)

const (
	defaultICMPSendInterval = 10 * time.Millisecond
	maximumICMPBufferBytes  = 1500
)

func (p *NetworkProber) probeICMP(
	ctx context.Context,
	target string,
	family string,
	packetCount int,
) (icmpProbeStats, error) {
	stats := icmpProbeStats{}
	address, err := p.resolve(ctx, target, family)
	if err != nil {
		return stats, err
	}
	if err := validateDialAddress(address); err != nil {
		return stats, err
	}
	if packetCount < 1 {
		packetCount = 1
	}
	if packetCount > model.MaxNetworkPacketCount {
		packetCount = model.MaxNetworkPacketCount
	}
	network, listenAddress, protocol := "udp4", "0.0.0.0", 1
	var messageType icmp.Type = ipv4.ICMPTypeEcho
	if address.Is6() {
		network, listenAddress, protocol = "udp6", "::", 58
		messageType = ipv6.ICMPTypeEchoRequest
	}
	connection, err := icmp.ListenPacket(network, listenAddress)
	if err != nil {
		return stats, err
	}
	stopClose := context.AfterFunc(ctx, func() { _ = connection.Close() })
	defer func() {
		stopClose()
		_ = connection.Close()
	}()
	if deadline, ok := ctx.Deadline(); ok {
		if err := connection.SetDeadline(deadline); err != nil {
			return stats, err
		}
	}
	destination := &net.UDPAddr{IP: net.IP(address.AsSlice())}
	baseSequence := os.Getpid() & 0xffff
	sentAt := make([]time.Time, 0, packetCount)
	seen := make([]bool, 0, packetCount)
	for index := 0; index < packetCount; index++ {
		if err := ctx.Err(); err != nil {
			if stats.sent == 0 {
				return stats, err
			}
			break
		}
		sequence := (baseSequence + index) & 0xffff
		message := icmp.Message{Type: messageType, Code: 0,
			Body: &icmp.Echo{ID: baseSequence, Seq: sequence, Data: []byte("beat")}}
		payload, marshalErr := message.Marshal(nil)
		if marshalErr != nil {
			return stats, marshalErr
		}
		if _, writeErr := connection.WriteTo(payload, destination); writeErr != nil {
			if stats.sent == 0 {
				return stats, writeErr
			}
			break
		}
		stats.sent++
		sentAt = append(sentAt, p.now())
		seen = append(seen, false)
		if index+1 < packetCount {
			timer := time.NewTimer(defaultICMPSendInterval)
			select {
			case <-ctx.Done():
				timer.Stop()
				return stats, ctx.Err()
			case <-timer.C:
			}
		}
	}
	buffer := make([]byte, maximumICMPBufferBytes)
	var lastErr error
	for stats.received < stats.sent {
		count, _, readErr := connection.ReadFrom(buffer)
		if readErr != nil {
			lastErr = readErr
			if ctx.Err() != nil {
				lastErr = ctx.Err()
			}
			if stats.received > 0 {
				return stats, nil
			}
			return stats, lastErr
		}
		reply, parseErr := icmp.ParseMessage(protocol, buffer[:count])
		if parseErr != nil {
			continue
		}
		echo, ok := reply.Body.(*icmp.Echo)
		if !ok {
			continue
		}
		index := (int(echo.Seq) - baseSequence) & 0xffff
		if index < 0 || index >= stats.sent || seen[index] {
			continue
		}
		seen[index] = true
		stats.received++
		stats.rtts = append(stats.rtts, p.now().Sub(sentAt[index]))
	}
	return stats, nil
}
