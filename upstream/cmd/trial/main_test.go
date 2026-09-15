package main

import (
	"errors"
	"testing"
)

// TestClassifyTrial ClaimTrial 结果归一化：错误 → FAIL；claimed → OK；
// 幂等已领（claimed=false, err=nil）→ ALREADY（不算失败）。
func TestClassifyTrial(t *testing.T) {
	cases := []struct {
		name    string
		claimed bool
		err     error
		want    trialStatus
	}{
		{name: "granted", claimed: true, err: nil, want: trialOK},
		{name: "already", claimed: false, err: nil, want: trialAlready},
		{name: "fail", claimed: false, err: errors.New("boom"), want: trialFailed},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			status, _ := classifyTrial(c.claimed, c.err)
			if status != c.want {
				t.Errorf("classifyTrial(%v,%v)=%q want %q", c.claimed, c.err, status, c.want)
			}
		})
	}
}

// TestClassifyTrialDetail 已领与失败的 detail 可读（幂等不为成片标红提供依据）。
func TestClassifyTrialDetail(t *testing.T) {
	if _, d := classifyTrial(false, nil); d == "" {
		t.Error("already 应填 detail")
	}
	if _, d := classifyTrial(false, errors.New("boom")); d != "boom" {
		t.Errorf("fail detail=%q want boom", d)
	}
}
