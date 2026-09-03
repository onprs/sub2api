package service

import (
	"bytes"
	"strconv"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// stripEmptyChatToolCallIdentity 从单个 CC 流式 chunk payload 中删除
// choices[*].delta.tool_calls[*] 上存在但为空字符串的 id 与
// function.name 字段；arguments（即使是空串）、index、type 与其它字段
// 一律保留，非空 id/name 不动。多 choice、多 index 都会处理。
//
// 返回 (原始 payload, false) 当：payload 为空、不含 "tool_calls"、
// 非法 JSON、无 choices / delta / tool_calls 数组、或没有需要删除的
// 字段。sjson 删除失败时 fail-closed 返回原始 payload。
func stripEmptyChatToolCallIdentity(payload []byte) ([]byte, bool) {
	if len(payload) == 0 {
		return payload, false
	}
	// 热路径快速失败：绝大多数 chunk 没有 tool_calls。
	if !bytes.Contains(payload, []byte("tool_calls")) {
		return payload, false
	}
	if !gjson.ValidBytes(payload) {
		return payload, false
	}
	choices := gjson.GetBytes(payload, "choices")
	if !choices.Exists() || !choices.IsArray() {
		return payload, false
	}
	updated := payload
	changed := false
	for ci, choice := range choices.Array() {
		delta := choice.Get("delta")
		if !delta.Exists() || !delta.IsObject() {
			continue
		}
		toolCalls := delta.Get("tool_calls")
		if !toolCalls.Exists() || !toolCalls.IsArray() {
			continue
		}
		for ti, tc := range toolCalls.Array() {
			if id := tc.Get("id"); id.Exists() && id.Type == gjson.String && id.Str == "" {
				next, err := sjson.DeleteBytes(updated, "choices."+strconv.Itoa(ci)+".delta.tool_calls."+strconv.Itoa(ti)+".id")
				if err != nil {
					return payload, false
				}
				updated = next
				changed = true
			}
			if name := tc.Get("function.name"); name.Exists() && name.Type == gjson.String && name.Str == "" {
				next, err := sjson.DeleteBytes(updated, "choices."+strconv.Itoa(ci)+".delta.tool_calls."+strconv.Itoa(ti)+".function.name")
				if err != nil {
					return payload, false
				}
				updated = next
				changed = true
			}
		}
	}
	if !changed {
		return payload, false
	}
	return updated, true
}
