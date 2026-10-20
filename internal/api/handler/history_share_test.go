package handler

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestHistoryShareCoalescesConcurrentReads(t *testing.T) {
	share := newHistoryShare[int]()
	release := make(chan struct{})
	started := make(chan struct{})
	var once sync.Once
	var calls atomic.Int64
	read := func(context.Context) (int, error) {
		calls.Add(1)
		once.Do(func() { close(started) })
		<-release
		return 42, nil
	}

	var wait sync.WaitGroup
	var ready sync.WaitGroup
	results := make(chan int, 4)
	errs := make(chan error, 4)
	leader := func() {
		defer wait.Done()
		value, err := share.do(context.Background(), "key", read)
		results <- value
		errs <- err
	}
	wait.Add(1)
	go leader()
	<-started
	for range 3 {
		wait.Add(1)
		ready.Add(1)
		go func() {
			ready.Done()
			leader()
		}()
	}
	ready.Wait()
	// 等待方进入 join 之前 leader 不能收尾，否则合并无从发生。
	time.Sleep(50 * time.Millisecond)
	close(release)
	wait.Wait()
	close(results)
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
	}
	count := 0
	for value := range results {
		count++
		if expected, actual := 42, value; expected != actual {
			t.Fatalf("expected %d, got %d", expected, actual)
		}
	}
	if expected, actual := 4, count; expected != actual {
		t.Fatalf("expected %d results, got %d", expected, actual)
	}
	if expected, actual := int64(1), calls.Load(); expected != actual {
		t.Fatalf("expected %d storage read, got %d", expected, actual)
	}
}

func TestHistoryShareKeepsDistinctKeysIndependent(t *testing.T) {
	share := newHistoryShare[int]()
	var calls atomic.Int64
	read := func(context.Context) (int, error) {
		return int(calls.Add(1)), nil
	}

	first, err := share.do(context.Background(), "a", read)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	second, err := share.do(context.Background(), "b", read)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if expected, actual := 1, first; expected != actual {
		t.Fatalf("expected %d, got %d", expected, actual)
	}
	if expected, actual := 2, second; expected != actual {
		t.Fatalf("expected %d, got %d", expected, actual)
	}
	if expected, actual := int64(2), calls.Load(); expected != actual {
		t.Fatalf("expected %d storage reads, got %d", expected, actual)
	}
}

func TestHistoryShareFallsBackWhenTableFull(t *testing.T) {
	share := newHistoryShare[int]()
	for index := range historyShareCapacity {
		if _, leader := share.join(historyShareKey("node", string(rune(index)))); !leader {
			t.Fatalf("expected key %d to register a call", index)
		}
	}
	if call, _ := share.join("overflow"); call != nil {
		t.Fatal("expected the full table to refuse a new call")
	}

	var calls atomic.Int64
	value, err := share.do(context.Background(), "overflow", func(context.Context) (int, error) {
		return int(calls.Add(1)) + 100, nil
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if expected, actual := 101, value; expected != actual {
		t.Fatalf("expected %d, got %d", expected, actual)
	}
	if expected, actual := int64(1), calls.Load(); expected != actual {
		t.Fatalf("expected %d storage read, got %d", expected, actual)
	}
}

func TestHistoryShareFollowerHonoursCancellation(t *testing.T) {
	share := newHistoryShare[int]()
	release := make(chan struct{})
	started := make(chan struct{})
	var once sync.Once
	read := func(context.Context) (int, error) {
		once.Do(func() { close(started) })
		<-release
		return 5, nil
	}

	leaderDone := make(chan error, 1)
	go func() {
		_, err := share.do(context.Background(), "key", read)
		leaderDone <- err
	}()
	<-started

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := share.do(cancelled, "key", read); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}

	close(release)
	if err := <-leaderDone; err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestHistorySharePropagatesReadErrorAndReleasesKey(t *testing.T) {
	share := newHistoryShare[int]()
	expected := errors.New("read failed")
	var calls atomic.Int64
	read := func(context.Context) (int, error) {
		calls.Add(1)
		return 0, expected
	}

	if _, err := share.do(context.Background(), "key", read); !errors.Is(err, expected) {
		t.Fatalf("expected %v, got %v", expected, err)
	}
	share.mu.Lock()
	remaining := len(share.calls)
	share.mu.Unlock()
	if expected, actual := 0, remaining; expected != actual {
		t.Fatalf("expected %d in-flight calls, got %d", expected, actual)
	}
	if _, err := share.do(context.Background(), "key", read); !errors.Is(err, expected) {
		t.Fatalf("expected %v, got %v", expected, err)
	}
	if expected, actual := int64(2), calls.Load(); expected != actual {
		t.Fatalf("expected %d storage reads, got %d", expected, actual)
	}
}

func TestHistoryShareNilReceiverReadsThrough(t *testing.T) {
	var share *historyShare[int]
	value, err := share.do(context.Background(), "key", func(context.Context) (int, error) {
		return 9, nil
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if expected, actual := 9, value; expected != actual {
		t.Fatalf("expected %d, got %d", expected, actual)
	}
}

func TestHistoryShareKeySeparatesFields(t *testing.T) {
	if expected, actual := historyShareKey("a", "bc"), "a\x00bc"; expected != actual {
		t.Fatalf("expected %q, got %q", expected, actual)
	}
	if expected, actual := historyShareKey("ab", "c"), "ab\x00c"; expected != actual {
		t.Fatalf("expected %q, got %q", expected, actual)
	}
	if expected, actual := historyShareKey("a", "bc"), historyShareKey("ab", "c"); expected == actual {
		t.Fatal("expected distinct field boundaries to produce distinct keys")
	}
}

func TestHistoryShareFollowerWaitsForLeaderResult(t *testing.T) {
	share := newHistoryShare[int]()
	release := make(chan struct{})
	started := make(chan struct{})
	var once sync.Once
	read := func(context.Context) (int, error) {
		once.Do(func() { close(started) })
		<-release
		return 11, nil
	}

	leaderDone := make(chan error, 1)
	go func() {
		_, err := share.do(context.Background(), "key", read)
		leaderDone <- err
	}()
	<-started

	follower := make(chan int, 1)
	go func() {
		value, err := share.do(context.Background(), "key", read)
		if err != nil {
			follower <- -1
			return
		}
		follower <- value
	}()

	select {
	case value := <-follower:
		t.Fatalf("follower returned %d before the leader finished", value)
	case <-time.After(10 * time.Millisecond):
	}
	close(release)
	if err := <-leaderDone; err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if expected, actual := 11, <-follower; expected != actual {
		t.Fatalf("expected %d, got %d", expected, actual)
	}
}
