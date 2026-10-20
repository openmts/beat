package geoip

import (
	"context"
	"net/http"
)

// ConfigureTestHTTP 仅供外部包测试注入内存传输；生产不接受自定义provider地址。
func ConfigureTestHTTP(service *Service, client *http.Client) { service.client = client }

func CompleteClaimForTest(service *Service, ctx context.Context, mutate func() error) error {
	item, err := service.claim(ctx)
	if err != nil {
		return err
	}
	if item == nil {
		return ErrNotFound
	}
	if err = mutate(); err != nil {
		return err
	}
	return service.complete(ctx, *item, resolution{scope: "public", country: "US", resolved: service.now()})
}
