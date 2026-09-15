// Package oauth implements the device authorization protocol shared by CLI and Web.
package oauth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"time"

	"workbuddy2api/internal/auth"
)

var ErrPending = errors.New("等待浏览器完成授权")
var ErrTemporary = errors.New("上游暂时不可用，请稍后重试")
var uidPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

func ValidUID(uid string) bool { return uidPattern.MatchString(uid) }

type Client struct {
	HTTP                *http.Client
	Base, Origin, Realm string
}

func RealmConfig(realm string) (string, string) {
	if realm == "global" {
		return "https://www.workbuddy.ai", "https://www.workbuddy.ai"
	}
	return "https://copilot.tencent.com", "https://www.codebuddy.cn"
}

func New(realm string) (*Client, error) {
	if realm != "cn" && realm != "global" {
		return nil, errors.New("请选择国内版或国际版")
	}
	base, origin := RealmConfig(realm)
	jar, _ := cookiejar.New(nil)
	return &Client{HTTP: &http.Client{Timeout: 20 * time.Second, Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, Base: base, Origin: origin, Realm: realm}, nil
}

func Headers(origin string) func(*http.Request) {
	return func(r *http.Request) {
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Accept", "application/json, text/plain, */*")
		r.Header.Set("X-Requested-With", "XMLHttpRequest")
		r.Header.Set("Origin", origin)
		r.Header.Set("Referer", origin+"/")
		r.Header.Set("User-Agent", "CLI/2.63.2 CodeBuddy/2.63.2")
	}
}

func ValidAuthorizationURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Host == "" || u.Port() != "" && u.Port() != "443" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	for _, domain := range []string{"codebuddy.cn", "codebuddy.ai", "workbuddy.ai", "tencent.com"} {
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return true
		}
	}
	return false
}

type envelope struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}
type apiError struct {
	code int
	msg  string
}

func (e *apiError) Error() string { return fmt.Sprintf("上游拒绝请求（code=%d）", e.code) }

func (c *Client) request(ctx context.Context, method, path, token string, body any) (envelope, error) {
	var out envelope
	var data []byte
	if body != nil {
		var err error
		data, err = json.Marshal(body)
		if err != nil {
			return out, err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, c.Base+path, bytes.NewReader(data))
	if err != nil {
		return out, errors.New("授权请求地址无效")
	}
	Headers(c.Origin)(req)
	if uid := req.URL.Query().Get("userId"); uid != "" {
		req.Header.Set("X-User-Id", uid)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return out, ErrTemporary
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil {
		return out, ErrTemporary
	}
	if len(raw) > 1<<20 {
		return out, errors.New("上游响应过大")
	}
	if resp.StatusCode == 429 || resp.StatusCode >= 500 {
		return out, ErrTemporary
	}
	if resp.StatusCode >= 300 {
		return out, fmt.Errorf("授权服务返回 HTTP %d", resp.StatusCode)
	}
	var shape map[string]json.RawMessage
	if json.Unmarshal(raw, &shape) != nil || shape["code"] == nil || json.Unmarshal(raw, &out) != nil {
		return out, errors.New("上游返回了无效的授权数据")
	}
	return out, nil
}

func (c *Client) Start(ctx context.Context) (state, authURL string, err error) {
	e, err := c.request(ctx, "POST", "/v2/plugin/auth/state?platform=CLI", "", map[string]any{})
	if err != nil {
		return "", "", err
	}
	if e.Code != 0 {
		return "", "", &apiError{e.Code, e.Msg}
	}
	var st struct {
		State string `json:"state"`
		URL   string `json:"authUrl"`
	}
	if json.Unmarshal(e.Data, &st) != nil || st.State == "" || len(st.State) > 4096 || st.URL == "" {
		return "", "", errors.New("上游未返回授权链接")
	}
	return st.State, st.URL, nil
}

func (c *Client) Poll(ctx context.Context, state string) (*auth.Auth, error) {
	query := "?state=" + url.QueryEscape(state)
	e, err := c.request(ctx, "GET", "/v2/plugin/auth/token"+query, "", nil)
	if err != nil {
		return nil, err
	}
	if e.Code != 0 {
		msg := strings.ToLower(strings.TrimSpace(e.Msg))
		// The token endpoint returns 11217 while the browser is still logging in
		// (observed msg: "11217:login ing..."). Classify by code, not that wording.
		if e.Code == 11217 || msg == "login ing" || msg == "waiting for login" || msg == "登录中" || msg == "等待登录" {
			return nil, ErrPending
		}
		return nil, &apiError{e.Code, e.Msg}
	}
	var tok struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
		ExpiresIn    int64  `json:"expiresIn"`
		Domain       string `json:"domain"`
	}
	if json.Unmarshal(e.Data, &tok) != nil || tok.AccessToken == "" || tok.RefreshToken == "" || tok.ExpiresIn <= 0 || tok.ExpiresIn > 366*86400 {
		return nil, errors.New("上游未返回完整登录凭据")
	}
	e, err = c.request(ctx, "GET", "/v2/plugin/login/account"+query, tok.AccessToken, nil)
	if err != nil {
		return nil, err
	}
	if e.Code != 0 {
		return nil, &apiError{e.Code, e.Msg}
	}
	var acct struct {
		UID          string `json:"uid"`
		EnterpriseID string `json:"enterpriseId"`
		Nickname     string `json:"nickname"`
	}
	if json.Unmarshal(e.Data, &acct) != nil || !ValidUID(acct.UID) {
		return nil, errors.New("上游未返回有效账号 ID")
	}
	raw, _ := json.Marshal(map[string]any{"auth": map[string]any{"accessToken": tok.AccessToken, "refreshToken": tok.RefreshToken, "expiresAt": time.Now().Unix() + tok.ExpiresIn, "domain": tok.Domain, "realm": c.Realm}, "account": acct})
	return auth.Parse(raw)
}

type Country struct {
	Code   any    `json:"Code"`
	Name   string `json:"Name"`
	EnName string `json:"EnName"`
	IOS2   string `json:"IOS2"`
}

// Registration only sends a region chosen by the user from the upstream list.
func (c *Client) Registration(ctx context.Context, a *auth.Auth) (bool, error) {
	e, err := c.request(ctx, "GET", "/auth/realms/copilot/overseas/user/register?userId="+url.QueryEscape(a.UID), a.AccessToken, nil)
	if err != nil {
		return false, err
	}
	if e.Code == 200 {
		return false, nil
	}
	if e.Code == 500 || strings.Contains(strings.ToLower(e.Msg), "region required") {
		return true, nil
	}
	return false, &apiError{e.Code, e.Msg}
}

func (c *Client) Countries(ctx context.Context) ([]Country, error) {
	e, err := c.request(ctx, "POST", "/billing/area/get-country-code", "", map[string]int{"filterForbidden": 1})
	if err != nil {
		return nil, err
	}
	if e.Code != 0 {
		return nil, &apiError{e.Code, e.Msg}
	}
	raw := e.Data
	var encoded string
	if json.Unmarshal(raw, &encoded) == nil {
		raw = []byte(encoded)
	}
	var inner struct {
		Code int `json:"code"`
		Data struct {
			List []Country `json:"list"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &inner) != nil || inner.Code != 0 {
		return nil, errors.New("无法获取注册地区")
	}
	var out []Country
	for _, v := range inner.Data.List {
		if strings.Contains("|HK|MO|SG|TH|PH|MY|ID|", "|"+v.IOS2+"|") && len(v.IOS2) == 2 {
			out = append(out, v)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("上游暂无可选注册地区")
	}
	return out, nil
}

func (c *Client) CompleteRegion(ctx context.Context, a *auth.Auth, region Country) error {
	e, err := c.request(ctx, "POST", "/console/login/account", a.AccessToken, map[string]any{"attributes": map[string][]string{"countryCode": {fmt.Sprint(region.Code)}, "countryFullName": {region.EnName}, "countryName": {region.IOS2}}})
	if err != nil {
		return err
	}
	if e.Code != 0 {
		return &apiError{e.Code, e.Msg}
	}
	needs, err := c.Registration(ctx, a)
	if err != nil {
		return err
	}
	if needs {
		return errors.New("地区提交后上游仍要求完善，请重试")
	}
	return nil
}
