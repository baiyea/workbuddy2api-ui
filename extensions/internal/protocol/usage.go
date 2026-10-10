package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"

	"github.com/QuantumNous/new-api/relaykit/types"
)

// Counts are cumulative: only an explicitly observed value replaces an earlier one.
type usagePresence struct{ Input, Output, Total *int64 }

func (u *usagePresence) observe(raw []byte) error {
	var v struct {
		Usage *struct {
			Input  *int64 `json:"prompt_tokens"`
			Output *int64 `json:"completion_tokens"`
			Total  *int64 `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return err
	}
	if v.Usage == nil {
		return nil
	}
	for _, n := range []*int64{v.Usage.Input, v.Usage.Output, v.Usage.Total} {
		if n != nil && *n < 0 {
			return errors.New("invalid usage")
		}
	}
	if v.Usage.Input != nil {
		u.Input = v.Usage.Input
	}
	if v.Usage.Output != nil {
		u.Output = v.Usage.Output
	}
	if v.Usage.Total != nil {
		u.Total = v.Usage.Total
	}
	if u.Total == nil && u.Input != nil && u.Output != nil && *u.Input > math.MaxInt64-*u.Output {
		return errors.New("usage overflow")
	}
	return nil
}
func (u usagePresence) public(format types.RelayFormat) map[string]any {
	m := map[string]any{"input_tokens": u.Input, "output_tokens": u.Output}
	if format == types.RelayFormatOpenAIResponses {
		total := u.Total
		if total == nil && u.Input != nil && u.Output != nil {
			n := *u.Input + *u.Output
			total = &n
		}
		m["total_tokens"] = total
	}
	return m
}
func publicJSON(value any, usage usagePresence, format types.RelayFormat) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var fields map[string]any
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if err = d.Decode(&fields); err != nil {
		return nil, err
	}
	if fields == nil {
		return nil, errors.New("empty converted output")
	}
	// Replace only serialized usage; RelayKit's integer DTOs cannot represent absence.
	if _, ok := fields["usage"]; ok {
		fields["usage"] = usage.public(format)
	}
	if fields["type"] == "message_delta" || fields["type"] == "message" {
		fields["usage"] = usage.public(format)
	}
	for _, key := range []string{"message", "response"} {
		if nested, ok := fields[key].(map[string]any); ok {
			nested["usage"] = usage.public(format)
			if format == types.RelayFormatOpenAIResponses {
				nested["store"] = false
			}
		}
	}
	if format == types.RelayFormatOpenAIResponses && fields["object"] == "response" {
		fields["usage"] = usage.public(format)
		fields["store"] = false
	}
	return json.Marshal(fields)
}
