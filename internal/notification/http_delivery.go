package notification

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

const (
	deliveryTimeout              = 10 * time.Second
	maximumDeliveryResponseBytes = 64 * 1024
)

func newDeliveryHTTPClient() *http.Client {
	return &http.Client{Timeout: deliveryTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func (s *Service) doHTTP(request *http.Request, channelName string) error {
	response, err := s.httpClient.Do(request)
	if err != nil {
		return safeTransportFailure(channelName, err)
	}
	defer func() { _ = response.Body.Close() }()
	// 上游可能回显令牌或通知内容；响应体仅限量消费，不进入日志或管理状态。
	size, err := io.Copy(io.Discard, io.LimitReader(response.Body, maximumDeliveryResponseBytes+1))
	if err != nil {
		return fmt.Errorf("%s response could not be read", channelName)
	}
	if size > maximumDeliveryResponseBytes {
		return fmt.Errorf("%s response exceeds the size limit", channelName)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("%s returned status %d", channelName, response.StatusCode)
	}
	return nil
}

func safeTransportFailure(channel string, failure error) error {
	if errors.Is(failure, context.Canceled) {
		return fmt.Errorf("%s request canceled: %w", channel, context.Canceled)
	}
	if errors.Is(failure, context.DeadlineExceeded) {
		return fmt.Errorf("%s request timed out: %w", channel, context.DeadlineExceeded)
	}
	var timeout net.Error
	if errors.As(failure, &timeout) && timeout.Timeout() {
		return fmt.Errorf("%s request timed out: %w", channel, context.DeadlineExceeded)
	}
	return fmt.Errorf("%s request failed: transport error", channel)
}
