package protocol

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/dto"
)

// Validate raw nested shapes before RelayKit decodes its any fields permissively.
func validateClaudeRequest(raw []byte, input *dto.ClaudeRequest) error {
	f, err := claudeObject(raw, "", "model max_tokens messages system stream temperature top_p top_k stop_sequences tools thinking output_config")
	if err != nil {
		return err
	}
	if strings.TrimSpace(input.Model) == "" {
		return errors.New("model 必须是非空字符串")
	}
	if input.MaxTokens == nil || *input.MaxTokens == 0 {
		return errors.New("max_tokens 必须是正整数")
	}
	for _, key := range []string{"temperature", "top_p"} {
		if v, ok := f[key]; ok {
			var n *float64
			upper := 1.0
			if key == "temperature" {
				upper = 2
			}
			if json.Unmarshal(v, &n) != nil || n == nil || *n < 0 || *n > upper {
				return errors.New(key + " 数值无效")
			}
		}
	}
	if v, ok := f["top_k"]; ok {
		var n *int
		if json.Unmarshal(v, &n) != nil || n == nil || *n < 0 {
			return errors.New("top_k 必须是非负整数")
		}
	}
	if v, ok := f["stop_sequences"]; ok {
		var list []*string
		if json.Unmarshal(v, &list) != nil || list == nil {
			return errors.New("stop_sequences 必须是字符串数组")
		}
		for i, s := range list {
			if s == nil {
				return fmt.Errorf("stop_sequences[%d] 必须是字符串", i)
			}
		}
	}
	if v, ok := f["system"]; ok {
		if err := validateClaudeContent(v, "system", "system"); err != nil {
			return err
		}
	}
	if v, ok := f["tools"]; ok {
		var list []json.RawMessage
		if json.Unmarshal(v, &list) != nil || list == nil {
			return errors.New("tools 必须是数组")
		}
		for i, raw := range list {
			p := fmt.Sprintf("tools[%d]", i)
			tool, err := claudeObject(raw, p, "name description input_schema")
			if err != nil {
				return err
			}
			if err := requiredString(tool, "name", p); err != nil {
				return err
			}
			if v, ok := tool["description"]; ok {
				if err := claudeString(v, p+".description", false); err != nil {
					return err
				}
			}
			var schema map[string]json.RawMessage
			if json.Unmarshal(tool["input_schema"], &schema) != nil || schema == nil {
				return errors.New(p + ".input_schema 必须是对象")
			}
		}
	}
	if v, ok := f["thinking"]; ok {
		thinking, err := claudeObject(v, "thinking", "type budget_tokens")
		if err != nil {
			return err
		}
		if err := requiredString(thinking, "type", "thinking"); err != nil {
			return err
		}
		var typ string
		_ = json.Unmarshal(thinking["type"], &typ)
		if typ != "enabled" && typ != "disabled" && typ != "adaptive" && typ != "between_tools" {
			return errors.New("thinking.type 无效")
		}
		if v, ok := thinking["budget_tokens"]; ok {
			var n *int
			if json.Unmarshal(v, &n) != nil || n == nil || *n <= 0 {
				return errors.New("thinking.budget_tokens 必须是正整数")
			}
		}
		if typ == "enabled" {
			if _, ok := thinking["budget_tokens"]; !ok {
				return errors.New("thinking.budget_tokens 必填")
			}
		}
	}
	if v, ok := f["output_config"]; ok {
		config, err := claudeObject(v, "output_config", "effort")
		if err != nil {
			return err
		}
		if err := requiredString(config, "effort", "output_config"); err != nil {
			return err
		}
	}
	var messages []json.RawMessage
	if json.Unmarshal(f["messages"], &messages) != nil || len(messages) == 0 {
		return errors.New("messages 必须是非空数组")
	}
	for i, raw := range messages {
		p := fmt.Sprintf("messages[%d]", i)
		msg, err := claudeObject(raw, p, "role content")
		if err != nil {
			return err
		}
		var role string
		_ = json.Unmarshal(msg["role"], &role)
		if role != "user" && role != "assistant" {
			return errors.New(p + ".role 无效")
		}
		if err := validateClaudeContent(msg["content"], p+".content", role); err != nil {
			return err
		}
	}
	return nil
}

func claudeObject(raw []byte, path, allowed string) (map[string]json.RawMessage, error) {
	var f map[string]json.RawMessage
	if json.Unmarshal(raw, &f) != nil || f == nil {
		return nil, errors.New(path + " 必须是对象")
	}
	for key := range f {
		if !slices.Contains(strings.Fields(allowed), key) {
			return nil, errors.New(strings.TrimPrefix(path+"."+key, ".") + " 不受支持")
		}
	}
	return f, nil
}

func claudeString(raw []byte, path string, nonempty bool) error {
	var s *string
	if json.Unmarshal(raw, &s) != nil || s == nil || (nonempty && strings.TrimSpace(*s) == "") {
		return errors.New(path + " 必须是字符串")
	}
	return nil
}

func validateClaudeContent(raw []byte, path, role string) error {
	var text *string
	if json.Unmarshal(raw, &text) == nil && text != nil {
		return nil
	}
	var blocks []json.RawMessage
	if json.Unmarshal(raw, &blocks) != nil || len(blocks) == 0 {
		return errors.New(path + " 必须是文本或非空内容数组")
	}
	for i, raw := range blocks {
		p := fmt.Sprintf("%s[%d]", path, i)
		var head struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(raw, &head) != nil {
			return errors.New(p + " 必须是内容对象")
		}
		allowed := "type"
		switch head.Type {
		case "text":
			allowed += " text"
		case "image", "document":
			if role == "system" {
				return errors.New(p + " 仅支持文本")
			}
			allowed += " source"
		case "thinking":
			if role != "assistant" {
				return errors.New(p + " thinking 仅用于 assistant")
			}
			allowed += " thinking signature"
		case "tool_use":
			if role != "assistant" {
				return errors.New(p + " tool_use 仅用于 assistant")
			}
			allowed += " id name input"
		case "tool_result":
			if role != "user" {
				return errors.New(p + " tool_result 仅用于 user")
			}
			allowed += " tool_use_id content is_error"
		default:
			return errors.New(p + ".type 不受支持")
		}
		f, err := claudeObject(raw, p, allowed)
		if err != nil {
			return err
		}
		switch head.Type {
		case "text":
			if err := claudeString(f["text"], p+".text", false); err != nil {
				return err
			}
		case "thinking":
			if err := claudeString(f["thinking"], p+".thinking", false); err != nil {
				return err
			}
			if v, ok := f["signature"]; ok {
				var s *string
				if json.Unmarshal(v, &s) != nil || s == nil || *s != "" {
					return errors.New(p + ".signature 不支持签名历史")
				}
			}
		case "tool_use":
			for _, key := range []string{"id", "name"} {
				if err := requiredString(f, key, p); err != nil {
					return err
				}
			}
			var input map[string]json.RawMessage
			if json.Unmarshal(f["input"], &input) != nil || input == nil {
				return errors.New(p + ".input 必须是对象")
			}
		case "tool_result":
			if err := requiredString(f, "tool_use_id", p); err != nil {
				return err
			}
			if v, ok := f["is_error"]; ok && string(v) != "false" {
				return errors.New(p + ".is_error 不支持错误语义转换")
			}
			if err := validateClaudeContent(f["content"], p+".content", "tool"); err != nil {
				return err
			}
		case "image", "document":
			if err := validateClaudeSource(f["source"], p+".source", head.Type); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateClaudeSource(raw []byte, path, kind string) error {
	f, err := claudeObject(raw, path, "type media_type data url")
	if err != nil {
		return err
	}
	var typ, media string
	_ = json.Unmarshal(f["type"], &typ)
	_ = json.Unmarshal(f["media_type"], &media)
	switch typ {
	case "url":
		if kind != "image" {
			return errors.New(path + ".type 不支持文档 URL")
		}
		if _, ok := f["data"]; ok {
			return errors.New(path + ".data 不受支持")
		}
		var s string
		if json.Unmarshal(f["url"], &s) != nil {
			return errors.New(path + ".url 无效")
		}
		u, err := url.Parse(s)
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
			return errors.New(path + ".url 必须是 HTTP(S) URL")
		}
	case "text":
		if kind != "document" || media != "text/plain" {
			return errors.New(path + ".media_type 无效")
		}
		if _, ok := f["url"]; ok {
			return errors.New(path + ".url 不受支持")
		}
		return claudeString(f["data"], path+".data", false)
	case "base64":
		if _, ok := f["url"]; ok {
			return errors.New(path + ".url 不受支持")
		}
		if kind == "image" {
			if media != "image/png" && media != "image/jpeg" && media != "image/gif" && media != "image/webp" {
				return errors.New(path + ".media_type 无效")
			}
		} else if media != "application/pdf" {
			return errors.New(path + ".media_type 仅支持 application/pdf")
		}
		var s string
		if json.Unmarshal(f["data"], &s) != nil || s == "" {
			return errors.New(path + ".data 必须是 base64 字符串")
		}
		if _, err := base64.StdEncoding.Strict().DecodeString(s); err != nil {
			return errors.New(path + ".data 无效 base64")
		}
	default:
		return errors.New(path + ".type 不受支持")
	}
	return nil
}
