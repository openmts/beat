package agent

import (
	"context"
	"fmt"
	"net"
)

func (p *NetworkProber) probeTCP(ctx context.Context, target, family string) error {
	host, port, err := net.SplitHostPort(target)
	if err != nil || host == "" || port == "" {
		return errInvalidProbeTarget
	}
	address, err := p.resolve(ctx, host, family)
	if err != nil {
		return err
	}
	if err := validateDialAddress(address); err != nil {
		return err
	}
	connection, err := (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort(address.String(), port))
	if err != nil {
		return err
	}
	if err := connection.Close(); err != nil {
		return fmt.Errorf("close TCP probe connection: %w", err)
	}
	return nil
}
