package upstream

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"
	"workbuddy2api/internal/auth"
	"workbuddy2api/internal/pool"
)

func TestLateRefreshCannotOverwriteReauthorizationWithSameRefreshToken(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		fmt.Fprint(w, `{"code":0,"data":{"accessToken":"stale-result","refreshToken":"stale-refresh","expiresIn":3600}}`)
	}))
	defer ts.Close()
	a := &auth.Auth{UID: "one", AccessToken: "before", RefreshToken: "unchanged", FilePath: filepath.Join(t.TempDir(), "workbuddy-one.json")}
	c := New()
	c.ChatBaseCN = ts.URL
	c.HTTP = ts.Client()
	done := make(chan error, 1)
	go func() { done <- c.RefreshToken(a) }()
	<-started
	next := &auth.Auth{UID: "one", AccessToken: "reauthorized", RefreshToken: "unchanged"}
	err := a.ReplaceAndSave(next)
	close(release)
	if err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if a.AccessToken != "reauthorized" {
		t.Fatal("late refresh overwrote new authorization")
	}
}

func TestRefreshPreservesExplicitRealmInCredentialSnapshot(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Origin") != "https://www.workbuddy.ai" || r.Header.Get("Accept-Language") != "en-US" {
			t.Errorf("explicit global realm lost in refresh headers: Origin=%s language=%s", r.Header.Get("Origin"), r.Header.Get("Accept-Language"))
		}
		if r.Header.Get("X-Refresh-Token") != "rt" {
			t.Error("refresh token changed")
		}
		fmt.Fprint(w, `{"code":0,"data":{"accessToken":"next","refreshToken":"next-r","expiresIn":3600}}`)
	}))
	defer ts.Close()
	a, err := auth.Parse([]byte(`{"uid":"one","accessToken":"at","refreshToken":"rt","realm":"global","domain":""}`))
	if err != nil {
		t.Fatal(err)
	}
	c := New()
	c.GlobalEnabled = true
	c.ChatBaseGlobal, c.HTTP = ts.URL, ts.Client()
	if err := c.RefreshToken(a); err != nil {
		t.Fatal(err)
	}
}

func TestChatRejectsActivationPendingAfterSelection(t *testing.T) {
	called := false
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	defer ts.Close()
	p := pool.New("")
	defer p.Close()
	a := &auth.Auth{UID: "one", AccessToken: "old", FilePath: filepath.Join(t.TempDir(), "workbuddy-one.json")}
	if err := p.Install(a); err != nil {
		t.Fatal(err)
	}
	picked := p.Pick("")
	if err := p.Install(&auth.Auth{UID: "one", AccessToken: "new", ActivationPending: true}); err != nil {
		t.Fatal(err)
	}
	c := New()
	c.ChatHTTP, c.ChatBaseCN = ts.Client(), ts.URL
	body, _, _, err := c.ChatStreamContext(context.Background(), picked, []byte(`{"messages":[]}`), "", ChatMeta{})
	if body != nil {
		body.Close()
	}
	if err == nil || called {
		t.Fatal("pending credentials sent after selection race")
	}
}

func TestPostLoginRequestsHonorCancellation(t *testing.T) {
	for _, operation := range []string{"checkin", "trial", "balance"} {
		t.Run(operation, func(t *testing.T) {
			started, release := make(chan struct{}), make(chan struct{})
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				close(started)
				select {
				case <-r.Context().Done():
				case <-release:
				}
			}))
			defer ts.Close()
			defer close(release)
			c := New()
			c.HTTP, c.BillingBaseCN, c.BillingBaseGlobal = ts.Client(), ts.URL, ts.URL
			a, _ := auth.Parse([]byte(`{"uid":"one","accessToken":"token","realm":"global"}`))
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				var err error
				switch operation {
				case "checkin":
					err = c.DailyCheckinContext(ctx, a)
				case "trial":
					_, err = c.ClaimTrialContext(ctx, a)
				case "balance":
					_, err = c.UserResourceContext(ctx, a)
				}
				done <- err
			}()
			select {
			case <-started:
			case <-time.After(2 * time.Second):
				t.Fatal("request did not start")
			}
			cancel()
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("canceled request reported success")
				}
			case <-time.After(time.Second):
				t.Fatal("request ignored cancellation")
			}
		})
	}
}

func TestReauthorizationConcurrentWithRequestHeaders(t *testing.T) {
	a := &auth.Auth{UID: "one", AccessToken: "before", RefreshToken: "rt", FilePath: filepath.Join(t.TempDir(), "workbuddy-one.json")}
	c := New()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 30; i++ {
			next := &auth.Auth{UID: "one", AccessToken: fmt.Sprint(i), RefreshToken: "rt", Domain: "cn", ExpiresAt: 3600}
			if e := a.ReplaceAndSave(next); e != nil {
				t.Error(e)
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			r, _ := http.NewRequest("GET", "http://example.test", nil)
			c.ChatHeaders(r, a, "", ChatMeta{})
			c.BillingHeaders(r, a)
			a.NeedsRefresh(time.Minute)
		}
	}()
	wg.Wait()
}
