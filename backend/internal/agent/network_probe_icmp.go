package agent

import (
	"context"
	"errors"
	"net"
	"os"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

func (p *NetworkProber) probeICMP(ctx context.Context, target, family string) error {
	address, err := p.resolve(ctx, target, family)
	if err != nil {
		return err
	}
	if err := validateDialAddress(address); err != nil {
		return err
	}
	network, listenAddress, protocol := "udp4", "0.0.0.0", 1
	var messageType icmp.Type = ipv4.ICMPTypeEcho
	if address.Is6() {
		network, listenAddress, protocol = "udp6", "::", 58
		messageType = ipv6.ICMPTypeEchoRequest
	}
	connection, err := icmp.ListenPacket(network, listenAddress)
	if err != nil {
		return err
	}
	stopClose := context.AfterFunc(ctx, func() { _ = connection.Close() })
	defer func() {
		stopClose()
		_ = connection.Close()
	}()
	if deadline, ok := ctx.Deadline(); ok {
		if err := connection.SetDeadline(deadline); err != nil {
			return err
		}
	}
	sequence := os.Getpid() & 0xffff
	message := icmp.Message{Type: messageType, Code: 0, Body: &icmp.Echo{ID: sequence, Seq: sequence, Data: []byte("beat")}}
	payload, err := message.Marshal(nil)
	if err != nil {
		return err
	}
	destination := &net.UDPAddr{IP: net.IP(address.AsSlice())}
	if _, err := connection.WriteTo(payload, destination); err != nil {
		return err
	}
	buffer := make([]byte, 1500)
	count, _, err := connection.ReadFrom(buffer)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	reply, err := icmp.ParseMessage(protocol, buffer[:count])
	if err != nil {
		return err
	}
	echo, ok := reply.Body.(*icmp.Echo)
	if !ok || echo.Seq != sequence {
		return errors.New("unexpected ICMP reply")
	}
	return nil
}
