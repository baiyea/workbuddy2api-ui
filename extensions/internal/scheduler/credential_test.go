package scheduler

import (
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"

	"workbuddy2api/internal/auth"
	"workbuddy2api/internal/pool"
	"workbuddy2api/internal/upstream"
)

type credentialTransport struct{ calls atomic.Int32 }

func (tr *credentialTransport) RoundTrip(*http.Request) (*http.Response, error) {
	tr.calls.Add(1)
	return nil, errors.New("mock upstream unavailable")
}

// The race detector catches an activity token check bypassing Auth's lock.
func TestActivityCredentialCheckConcurrentWithRefresh(t *testing.T) {
	p := pool.New("")
	defer p.Close()
	a := &auth.Auth{UID: "one", AccessToken: "before"}
	p.Add(a)
	tr := &credentialTransport{}
	up := upstream.New()
	up.HTTP = &http.Client{Transport: tr}
	s := New(Config{Pool: p, Upstream: up, ActivityReportCount: 1})
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-done:
				return
			default:
			}
			a.Lock()
			a.AccessToken = "refreshed"
			a.Unlock()
		}
	}()
	for i := 0; i < 100; i++ {
		s.RunActivityNow()
	}
	close(done)
	wg.Wait()
	if tr.calls.Load() != 100 {
		t.Fatalf("activity calls=%d", tr.calls.Load())
	}
}
