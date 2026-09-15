package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"workbuddy2api/internal/auth"
)

func TestAuthorizationWaitsThenRequiresCompleteAccount(t *testing.T) {
	pending, uid := true, "account-1"
	pendingResponse := `{"code":11217,"msg":"11217:login ing...","requestId":"test-request"}`
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Origin") != "https://www.codebuddy.cn" {
			t.Error("wrong realm origin")
		}
		switch r.URL.Path {
		case "/v2/plugin/auth/state":
			fmt.Fprint(w, `{"code":0,"data":{"state":"opaque","authUrl":"https://www.codebuddy.cn/login?state=opaque"}}`)
		case "/v2/plugin/auth/token":
			if r.URL.Query().Get("state") != "opaque" {
				t.Error("lost state")
			}
			if pending {
				fmt.Fprint(w, pendingResponse)
				return
			}
			fmt.Fprint(w, `{"code":0,"data":{"accessToken":"secret-at","refreshToken":"secret-rt","expiresIn":3600}}`)
		case "/v2/plugin/login/account":
			if r.Header.Get("Authorization") != "Bearer secret-at" {
				t.Error("missing bearer")
			}
			fmt.Fprintf(w, `{"code":0,"data":{"uid":%q,"nickname":"Tester"}}`, uid)
		}
	}))
	defer ts.Close()
	c, _ := New("cn")
	c.Base = ts.URL
	c.HTTP = ts.Client()
	state, _, err := c.Start(context.Background())
	if err != nil || state != "opaque" {
		t.Fatalf("start: %s %v", state, err)
	}
	for _, response := range []string{pendingResponse, `{"code":11217,"msg":""}`, `{"code":1,"msg":"login ing"}`} {
		pendingResponse = response
		if _, err = c.Poll(context.Background(), state); !errors.Is(err, ErrPending) {
			t.Fatalf("pending response %s: %v", response, err)
		}
	}
	pending = false
	a, err := c.Poll(context.Background(), state)
	if err != nil || a.UID != "account-1" || a.Realm() != "cn" {
		t.Fatalf("account: %v %v", a, err)
	}
	uid = "../escape"
	if _, err = c.Poll(context.Background(), state); err == nil {
		t.Fatal("accepted unsafe UID")
	}
	uid = ""
	if _, err = c.Poll(context.Background(), state); err == nil {
		t.Fatal("accepted missing account")
	}
}

func TestAuthorizationRejectsUntrustedLinksAndUnknownErrors(t *testing.T) {
	for _, link := range []string{"javascript:alert(1)", "https://www.codebuddy.cn.evil.test/", "http://www.codebuddy.cn/", "https://evil.test/"} {
		if ValidAuthorizationURL(link) {
			t.Errorf("accepted %s", link)
		}
	}
	for _, link := range []string{"https://www.codebuddy.cn/login", "https://www.workbuddy.ai/login"} {
		if !ValidAuthorizationURL(link) {
			t.Errorf("rejected %s", link)
		}
	}
	if _, err := New("bad"); err == nil {
		t.Fatal("accepted invalid realm")
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"code":777,"msg":"secret failure"}`) }))
	defer ts.Close()
	c, _ := New("cn")
	c.Base = ts.URL
	c.HTTP = ts.Client()
	if _, err := c.Poll(context.Background(), "state"); err == nil || errors.Is(err, ErrPending) {
		t.Fatalf("unknown error treated as pending: %v", err)
	}
}

func TestGlobalRegionSelectionIsExplicitAndActivationIsChecked(t *testing.T) {
	selected := false
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/realms/copilot/overseas/user/register":
			if r.Header.Get("X-User-Id") != "account-1" {
				t.Error("missing registration account header")
			}
			if selected {
				fmt.Fprint(w, `{"code":200}`)
			} else {
				fmt.Fprint(w, `{"code":500,"msg":"region required"}`)
			}
		case "/billing/area/get-country-code":
			fmt.Fprint(w, `{"code":0,"data":"{\"code\":0,\"data\":{\"list\":[{\"IOS2\":\"SG\",\"Code\":65,\"EnName\":\"Singapore\"},{\"IOS2\":\"XX\",\"Code\":1,\"EnName\":\"Invalid\"}]}}"}`)
		case "/console/login/account":
			if r.Header.Get("Authorization") != "Bearer at" {
				t.Error("missing token")
			}
			var body map[string]map[string][]string
			json.NewDecoder(r.Body).Decode(&body)
			if body["attributes"]["countryName"][0] != "SG" {
				t.Error("wrong selected region")
			}
			selected = true
			fmt.Fprint(w, `{"code":0}`)
		}
	}))
	defer ts.Close()
	c, _ := New("global")
	c.Base = ts.URL
	c.HTTP = ts.Client()
	a := &auth.Auth{UID: "account-1", AccessToken: "at"}
	needs, err := c.Registration(context.Background(), a)
	if err != nil || !needs {
		t.Fatal("missing required-region state")
	}
	countries, err := c.Countries(context.Background())
	if err != nil || len(countries) != 1 || countries[0].IOS2 != "SG" {
		t.Fatalf("countries: %v %v", countries, err)
	}
	if selected {
		t.Fatal("region changed before user selection")
	}
	if err := c.CompleteRegion(context.Background(), a, countries[0]); err != nil {
		t.Fatal(err)
	}
}
