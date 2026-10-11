package protocol

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

func validateResponses(fields map[string]json.RawMessage) error {
	for _, key := range []string{"previous_response_id", "conversation", "prompt", "context_management"} {
		if _, ok := fields[key]; ok {
			return errors.New(key + " 不支持有状态请求")
		}
	}
	for _, key := range []string{"store", "background"} {
		if string(fields[key]) == "true" {
			return errors.New(key + " 不支持 true")
		}
	}
	if v, ok := fields["instructions"]; ok {
		var s *string
		if json.Unmarshal(v, &s) != nil || s == nil {
			return errors.New("instructions 必须是文本")
		}
	}
	if v, ok := fields["max_output_tokens"]; ok {
		var n uint
		if json.Unmarshal(v, &n) != nil || n == 0 {
			return errors.New("max_output_tokens 必须是正整数")
		}
	}
	if v, ok := fields["tools"]; ok {
		if err := validateTools(v, "tools"); err != nil {
			return err
		}
	}
	if v, ok := fields["text"]; ok {
		var config map[string]json.RawMessage
		if json.Unmarshal(v, &config) != nil || config == nil {
			return errors.New("text 必须是对象")
		}
		if v, ok := config["format"]; ok {
			var format map[string]json.RawMessage
			if json.Unmarshal(v, &format) != nil || format == nil {
				return errors.New("text.format 必须是对象")
			}
			if err := requiredString(format, "type", "text.format"); err != nil {
				return err
			}
			if err := validateSchemaFields(format, "text.format", "schema"); err != nil {
				return err
			}
		}
	}
	var text *string
	if json.Unmarshal(fields["input"], &text) == nil && text != nil {
		return nil
	}
	var items []map[string]json.RawMessage
	if json.Unmarshal(fields["input"], &items) != nil || len(items) == 0 {
		return errors.New("input 必须是文本或非空数组")
	}
	for i, item := range items {
		path := fmt.Sprintf("input[%d]", i)
		if item == nil {
			return errors.New(path + " 必须是对象")
		}
		var typ string
		if v, ok := item["type"]; ok {
			if json.Unmarshal(v, &typ) != nil {
				return errors.New(path + ".type 无效")
			}
		}
		if typ == "" || typ == "message" {
			var role string
			_ = json.Unmarshal(item["role"], &role)
			if role != "user" && role != "assistant" && role != "system" && role != "developer" {
				return errors.New(path + ".role 无效")
			}
			if err := validateResponsesContent(item["content"], path+".content"); err != nil {
				return err
			}
		} else {
			switch typ {
			case "reasoning":
				if err := validateResponsesReasoning(item, path); err != nil {
					return err
				}
			case "function_call", "custom_tool_call":
				for _, key := range []string{"call_id", "name"} {
					if err := requiredString(item, key, path); err != nil {
						return err
					}
				}
				key := "arguments"
				if typ == "custom_tool_call" {
					key = "input"
				}
				var s *string
				if json.Unmarshal(item[key], &s) != nil || s == nil {
					return errors.New(path + "." + key + " 必须是字符串")
				}
			case "function_call_output", "custom_tool_call_output":
				if err := requiredString(item, "call_id", path); err != nil {
					return err
				}
				if err := validateResponsesContent(item["output"], path+".output"); err != nil {
					return err
				}
			case "additional_tools":
				if err := validateTools(item["tools"], path+".tools"); err != nil {
					return err
				}
			default:
				return errors.New(path + ".type 不受支持")
			}
		}
	}
	return nil
}

func validateResponsesReasoning(item map[string]json.RawMessage, path string) error {
	for key := range item {
		switch key {
		case "type", "summary":
		case "id", "status":
			if err := requiredString(item, key, path); err != nil {
				return err
			}
		case "content":
			if string(item[key]) != "null" {
				return errors.New(path + ".content 不支持：仅支持明文 summary")
			}
		case "role", "quality", "size":
			// RelayKit's shared output DTO emits these inert fields on reasoning items.
			var value *string
			if json.Unmarshal(item[key], &value) != nil || value == nil || *value != "" {
				return errors.New(path + "." + key + " 不受支持")
			}
		default:
			return errors.New(path + "." + key + " 不支持：仅支持明文 summary")
		}
	}
	var summary []map[string]json.RawMessage
	if json.Unmarshal(item["summary"], &summary) != nil || summary == nil {
		return errors.New(path + ".summary 必须是数组")
	}
	for i, block := range summary {
		p := fmt.Sprintf("%s.summary[%d]", path, i)
		for key := range block {
			if key != "type" && key != "text" {
				return errors.New(p + "." + key + " 不受支持")
			}
		}
		var typ string
		var text *string
		if json.Unmarshal(block["type"], &typ) != nil || typ != "summary_text" || json.Unmarshal(block["text"], &text) != nil || text == nil {
			return errors.New(p + " 必须是 summary_text 文本块")
		}
	}
	return nil
}

func requiredString(fields map[string]json.RawMessage, key, path string) error {
	var s *string
	if json.Unmarshal(fields[key], &s) != nil || s == nil || strings.TrimSpace(*s) == "" {
		return errors.New(path + "." + key + " 必须是非空字符串")
	}
	return nil
}
func validateSchemaFields(fields map[string]json.RawMessage, path, schemaKey string) error {
	if v, ok := fields["strict"]; ok && string(v) != "true" && string(v) != "false" {
		return errors.New(path + ".strict 必须是布尔值")
	}
	if v, ok := fields[schemaKey]; ok {
		var schema map[string]json.RawMessage
		if json.Unmarshal(v, &schema) != nil || schema == nil {
			return errors.New(path + "." + schemaKey + " 必须是对象")
		}
	}
	return nil
}
func validateTools(raw json.RawMessage, path string) error {
	var tools []map[string]json.RawMessage
	if json.Unmarshal(raw, &tools) != nil || tools == nil {
		return errors.New(path + " 必须是数组")
	}
	for i, tool := range tools {
		p := fmt.Sprintf("%s[%d]", path, i)
		if err := requiredString(tool, "type", p); err != nil {
			return err
		}
		var typ string
		_ = json.Unmarshal(tool["type"], &typ)
		if typ == "function" || typ == "custom" || typ == "namespace" {
			if err := requiredString(tool, "name", p); err != nil {
				return err
			}
		}
		if err := validateSchemaFields(tool, p, "parameters"); err != nil {
			return err
		}
		if typ == "namespace" {
			if err := validateTools(tool["tools"], p+".tools"); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateText(raw json.RawMessage, path, blockType string) error {
	var s *string
	if json.Unmarshal(raw, &s) == nil && s != nil {
		return nil
	}
	var blocks []struct {
		Type string  `json:"type"`
		Text *string `json:"text"`
	}
	if strictJSON(raw, &blocks) != nil || len(blocks) == 0 {
		return errors.New(path + " 必须是文本或文本块")
	}
	for _, b := range blocks {
		if b.Type != blockType || b.Text == nil {
			return errors.New(path + " 文本块无效")
		}
	}
	return nil
}

func validateResponsesContent(raw json.RawMessage, path string) error {
	var s *string
	if json.Unmarshal(raw, &s) == nil && s != nil {
		return nil
	}
	var blocks []map[string]json.RawMessage
	if json.Unmarshal(raw, &blocks) != nil || len(blocks) == 0 {
		return errors.New(path + " 必须是文本或内容数组")
	}
	for i, b := range blocks {
		p := fmt.Sprintf("%s[%d]", path, i)
		var typ string
		if b == nil || json.Unmarshal(b["type"], &typ) != nil {
			return errors.New(p + ".type 无效")
		}
		switch typ {
		case "input_text", "output_text", "text":
			var text *string
			if json.Unmarshal(b["text"], &text) != nil || text == nil {
				return errors.New(p + ".text 无效")
			}
		case "input_image":
			var url *string
			if json.Unmarshal(b["image_url"], &url) != nil || url == nil || *url == "" {
				return errors.New(p + ".image_url 无效")
			}
			if raw, ok := b["detail"]; ok {
				var detail string
				if json.Unmarshal(raw, &detail) != nil || (detail != "low" && detail != "high" && detail != "auto") {
					return errors.New(p + ".detail 必须为 low/high/auto")
				}
			}
		case "input_file":
			if _, ok := b["file_id"]; ok {
				return errors.New(p + ".file_id 不受支持")
			}
			found := false
			for _, key := range []string{"file_data", "file_url"} {
				if v, ok := b[key]; ok {
					var text *string
					if json.Unmarshal(v, &text) != nil || text == nil || *text == "" {
						return errors.New(p + "." + key + " 无效")
					}
					found = true
				}
			}
			if !found {
				return errors.New(p + " 缺少文件内容")
			}
		default:
			return errors.New(p + ".type 不受支持")
		}
	}
	return nil
}
