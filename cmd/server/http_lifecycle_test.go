package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestFatalWorkerCancelsActualHTTPContext(t *testing.T) {
	for _, panics := range []bool{false, true} {
		t.Run(map[bool]string{false: "exit", true: "panic"}[panics], func(t *testing.T) {
			root, cancel := context.WithCancel(t.Context())
			defer cancel()
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })}
			var closed atomic.Bool
			done := make(chan error, 1)
			go func() {
				done <- (httpLifecycle{server: server, listener: listener, beforeShutdown: func() { closed.Store(true) }}).run(root)
			}()
			response, err := http.Get("http://" + listener.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			_, copyErr := io.Copy(io.Discard, response.Body)
			closeErr := response.Body.Close()
			if copyErr != nil || closeErr != nil {
				t.Fatal(copyErr, closeErr)
			}
			supervisor := newWorkerSupervisor()
			supervisor.bind(root, cancel)
			worker := supervisor.register("alerts", time.Second)
			var wait sync.WaitGroup
			supervisor.start(&wait, worker, func(context.Context) {
				if panics {
					panic("redacted")
				}
			})
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("HTTP ignored supervisor root cancellation")
			}
			wait.Wait()
			if !closed.Load() || t.Context().Err() != nil || !supervisor.terminal.Load() {
				t.Fatal("incorrect lifecycle ownership")
			}
		})
	}
}

func TestHTTPStartupFailureReturnsWithoutLeakedListener(t *testing.T) {
	err := serveHTTP(t.Context(), httpServerConfig{address: "invalid address", handler: http.NotFoundHandler()})
	if err == nil {
		t.Fatal("invalid listener accepted")
	}
}
