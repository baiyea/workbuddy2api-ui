package upstream

import (
	"encoding/json"
	"testing"
)

// TestCleanupOrphanToolCallsNoTraffic 无工具流量 → 零改动（changed=false）。
func TestCleanupOrphanToolCallsNoTraffic(t *testing.T) {
	messages := []any{
		map[string]any{"role": "system", "content": "hi"},
		map[string]any{"role": "user", "content": "hello"},
		map[string]any{"role": "assistant", "content": "hi there"},
	}
	out, changed := cleanupOrphanToolCalls(messages)
	if changed {
		t.Fatal("no-traffic should be unchanged")
	}
	if &out[0] != &messages[0] {
		t.Fatal("no-traffic should return the original slice")
	}
}

// TestCleanupOrphanToolCallWithoutResult 孤儿 tool_call（无结果）→ 删除 tool_calls 键。
func TestCleanupOrphanToolCallWithoutResult(t *testing.T) {
	messages := []any{
		map[string]any{"role": "assistant", "content": nil, "tool_calls": []any{
			map[string]any{"id": "call_1", "type": "function", "function": map[string]any{"name": "Grep", "arguments": "{}"}},
		}},
		map[string]any{"role": "user", "content": "continue"},
	}
	out, changed := cleanupOrphanToolCalls(messages)
	if !changed {
		t.Fatal("orphan tool_call should be cleaned")
	}
	asst := out[0].(map[string]any)
	if _, ok := asst["tool_calls"]; ok {
		t.Fatalf("tool_calls should be removed, got %#v", asst)
	}
}

// TestCleanupOrphanPartialBatch 一批两个 tool_call，只有 c1 拿到结果 → 整批剔除。
func TestCleanupOrphanPartialBatch(t *testing.T) {
	messages := []any{
		map[string]any{"role": "assistant", "tool_calls": []any{
			map[string]any{"id": "c1", "type": "function", "function": map[string]any{"name": "read", "arguments": "{}"}},
			map[string]any{"id": "c2", "type": "function", "function": map[string]any{"name": "read", "arguments": "{}"}},
		}},
		map[string]any{"role": "tool", "tool_call_id": "c1", "content": "ok"},
		map[string]any{"role": "user", "content": "next"},
	}
	out, changed := cleanupOrphanToolCalls(messages)
	if !changed {
		t.Fatal("partial batch should be cleaned")
	}
	asst := out[0].(map[string]any)
	if _, ok := asst["tool_calls"]; ok {
		t.Fatalf("partial batch tool_calls should be dropped, got %#v", asst)
	}
}

// TestCleanupOrphanResultOnly 孤儿 tool 结果（无对应 tool_call）→ 整条消息删除。
func TestCleanupOrphanResultOnly(t *testing.T) {
	messages := []any{
		map[string]any{"role": "user", "content": "hi"},
		map[string]any{"role": "tool", "tool_call_id": "ghost", "content": "orphan"},
	}
	out, changed := cleanupOrphanToolCalls(messages)
	if !changed {
		t.Fatal("orphan result should be cleaned")
	}
	if len(out) != 1 || out[0].(map[string]any)["role"] != "user" {
		t.Fatalf("orphan tool message should be removed, got %#v", out)
	}
}

// TestCleanupOrphanPairingPreserved 正例零改动：完整配对的多 tool_call 轮 + 正常文本轮，
// 所有字段原样保留。
func TestCleanupOrphanPairingPreserved(t *testing.T) {
	grepArgs := `{"pattern":"foo","path":"."}`
	readArgs := `{"file_path":"a.go"}`
	messages := []any{
		map[string]any{"role": "user", "content": "search"},
		map[string]any{"role": "assistant", "content": nil, "tool_calls": []any{
			map[string]any{"id": "call_1", "type": "function", "function": map[string]any{"name": "Grep", "arguments": grepArgs}},
			map[string]any{"id": "call_2", "type": "function", "function": map[string]any{"name": "Read", "arguments": readArgs}},
		}},
		map[string]any{"role": "tool", "tool_call_id": "call_1", "content": "3 matches"},
		map[string]any{"role": "tool", "tool_call_id": "call_2", "content": "file body"},
		map[string]any{"role": "user", "content": "keep going"},
	}
	out, changed := cleanupOrphanToolCalls(messages)
	if changed {
		t.Fatal("fully paired round-trip must be zero-change")
	}
	if len(out) != 5 {
		t.Fatalf("message count changed: %d", len(out))
	}
	asst := out[1].(map[string]any)
	tcs := asst["tool_calls"].([]any)
	if len(tcs) != 2 {
		t.Fatalf("tool_calls dropped from paired round-trip: %#v", asst)
	}
	// 函数参数原样保留（引用不变）。
	fn := tcs[0].(map[string]any)["function"].(map[string]any)
	if fn["arguments"] != grepArgs {
		t.Fatalf("call_1 arguments mutated: %v", fn["arguments"])
	}
}

// TestCleanupOrphanOutOfOrderToolBeforeResult 乱序：tool 结果消息出现在 assistant
// tool_call 之前（不按顺序但 id 齐全）→ 仍保留（按 id 集合配对，与顺序无关）。
func TestCleanupOrphanOutOfOrderToolBeforeResult(t *testing.T) {
	messages := []any{
		map[string]any{"role": "tool", "tool_call_id": "call_9", "content": "res"},
		map[string]any{"role": "assistant", "tool_calls": []any{
			map[string]any{"id": "call_9", "type": "function", "function": map[string]any{"name": "f", "arguments": "{}"}},
		}},
	}
	out, changed := cleanupOrphanToolCalls(messages)
	if changed {
		t.Fatal("id-complete out-of-order pairing must be preserved")
	}
	if len(out) != 2 {
		t.Fatalf("message count changed: %d", len(out))
	}
}

// TestCleanupOrphanDuplicateID 重复 tool_call id（两处引用同一结果 id）：
// 结果侧唯一、调用侧重复——每次按集合取并，保持「id 命中结果即保留」的最宽口径。
func TestCleanupOrphanDuplicateID(t *testing.T) {
	messages := []any{
		map[string]any{"role": "tool", "tool_call_id": "dup", "content": "r"},
		map[string]any{"role": "assistant", "tool_calls": []any{
			map[string]any{"id": "dup", "type": "function", "function": map[string]any{"name": "a", "arguments": "{}"}},
		}},
		map[string]any{"role": "assistant", "tool_calls": []any{
			map[string]any{"id": "dup", "type": "function", "function": map[string]any{"name": "b", "arguments": "{}"}},
		}},
	}
	_, changed := cleanupOrphanToolCalls(messages)
	if changed {
		t.Fatal("duplicate id referencing an existing result must be preserved (widest keep)")
	}
}

// TestCleanupOrphanThroughPrepareBody 集成：孤儿 tool_call 经 PrepareBodyOpt 全链路被剔除，
// 且与消息顺序无关。
func TestCleanupOrphanThroughPrepareBody(t *testing.T) {
	body := `{"model":"glm-5.2","messages":[
		{"role":"assistant","tool_calls":[{"id":"bad","type":"function","function":{"name":"f","arguments":"{}"}}]},
		{"role":"user","content":"hi"}
	]}`
	out := PrepareBodyOpt([]byte(body), false)
	var obj map[string]any
	if err := json.Unmarshal(out, &obj); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	msgs := obj["messages"].([]any)
	asst := msgs[0].(map[string]any)
	if _, ok := asst["tool_calls"]; ok {
		t.Fatalf("orphan tool_calls should be stripped by PrepareBodyOpt, got %#v", asst)
	}
}
