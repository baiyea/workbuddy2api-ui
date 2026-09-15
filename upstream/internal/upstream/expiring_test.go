package upstream

import (
	"bytes"
	"io"
	"net/http"
	"testing"
	"time"

	"workbuddy2api/internal/auth"
)

// mkDetailedResp 构造带 PackageEndTime 的 get-user-resource 响应。
func mkDetailedResp(accounts string) *http.Response {
	body := `{"code":0,"data":{"Response":{"Data":{"TotalCount":1,"Accounts":[` + accounts + `]}}}}`
	return &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(bytes.NewReader([]byte(body))),
		Header:     make(http.Header),
	}
}

func TestUserResourceDetailedSplitsExpiring(t *testing.T) {
	now := time.Now()
	in3d := now.Add(3 * 24 * time.Hour).Format(packageEndLayout)
	in30d := now.Add(30 * 24 * time.Hour).Format(packageEndLayout)
	c := testClient(func(r *http.Request) (*http.Response, error) {
		return mkDetailedResp(
			`{"PackageName":"奖励包","PackageEndTime":"` + in3d + `","CycleCapacitySize":1500,"CycleCapacityRemain":1200,"CycleCapacityUsed":300},` +
				`{"PackageName":"周期包","PackageEndTime":"` + in30d + `","CycleCapacitySize":500,"CycleCapacityRemain":300,"CycleCapacityUsed":200}`), nil
	})
	a := &auth.Auth{AccessToken: "at", UID: "u1"}
	remain, buckets, err := c.UserResourceDetailed(a, 7*24*time.Hour)
	if err != nil {
		t.Fatalf("detailed: %v", err)
	}
	if remain != 1500 {
		t.Errorf("remain=%d want 1500", remain)
	}
	if buckets.Expiring != 1200 {
		t.Errorf("expiring=%d want 1200 (3天内过期的奖励包)", buckets.Expiring)
	}
	if buckets.Stable != 300 {
		t.Errorf("stable=%d want 300 (30天后才过期的周期包)", buckets.Stable)
	}
	if buckets.Total() != remain {
		t.Errorf("total=%d != remain=%d", buckets.Total(), remain)
	}
}

func TestUserResourceDetailedNoWindowAllStable(t *testing.T) {
	in3d := time.Now().Add(3 * 24 * time.Hour).Format(packageEndLayout)
	c := testClient(func(r *http.Request) (*http.Response, error) {
		return mkDetailedResp(
			`{"PackageName":"p","PackageEndTime":"` + in3d + `","CycleCapacitySize":100,"CycleCapacityRemain":80,"CycleCapacityUsed":20}`), nil
	})
	// soon<=0：禁用分桶，全部归 Stable（向后兼容旧行为）。
	_, buckets, err := c.UserResourceDetailed(&auth.Auth{AccessToken: "at"}, 0)
	if err != nil {
		t.Fatalf("detailed: %v", err)
	}
	if buckets.Expiring != 0 || buckets.Stable != 80 {
		t.Errorf("buckets=%+v want {Expiring:0 Stable:80} when soon=0", buckets)
	}
}

func TestUserResourceDetailedMissingEndTimeStable(t *testing.T) {
	c := testClient(func(r *http.Request) (*http.Response, error) {
		// 无 PackageEndTime 字段：保守归 Stable，不误标快过期插队。
		return mkDetailedResp(
			`{"PackageName":"p","CycleCapacitySize":100,"CycleCapacityRemain":80,"CycleCapacityUsed":20}`), nil
	})
	_, buckets, err := c.UserResourceDetailed(&auth.Auth{AccessToken: "at"}, 7*24*time.Hour)
	if err != nil {
		t.Fatalf("detailed: %v", err)
	}
	if buckets.Expiring != 0 || buckets.Stable != 80 {
		t.Errorf("buckets=%+v want {Expiring:0 Stable:80} for missing end time", buckets)
	}
}

func TestUserResourceBackwardCompat(t *testing.T) {
	// 旧 UserResource 签名与总量口径不变。
	c := testClient(func(r *http.Request) (*http.Response, error) {
		return mkDetailedResp(
			`{"PackageName":"p","CycleCapacitySize":100,"CycleCapacityRemain":80,"CycleCapacityUsed":20}`), nil
	})
	remain, err := c.UserResource(&auth.Auth{AccessToken: "at"})
	if err != nil || remain != 80 {
		t.Errorf("UserResource remain=%d err=%v, want 80/nil", remain, err)
	}
}
