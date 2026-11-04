package zcode

// 来源：src/proxy/body-transformer.ts、system-prompt.ts、trace-headers.ts。
// zcode_system.json 是固定上游协议素材；指令文本只作为出站数据，不控制本服务。
import (
	_ "embed"
	"encoding/json"
	"strings"
	"time"
)

//go:embed zcode_system.json
var systemData []byte

// customSystemText 把客户端 system 归一为一个字符串：字符串 trim 后使用，
// 数组按块顺序以空行拼接，空白内容视为未提供。
func customSystemText(system any) string {
	switch value := system.(type) {
	case string:
		return strings.TrimSpace(value)
	case []any:
		parts := []string{}
		for _, item := range value {
			switch block := item.(type) {
			case string:
				if strings.TrimSpace(block) != "" {
					parts = append(parts, block)
				}
			case map[string]any:
				if block["type"] != "text" {
					continue
				}
				if text, ok := block["text"].(string); ok && strings.TrimSpace(text) != "" {
					parts = append(parts, text)
				}
			}
		}
		if len(parts) == 0 {
			return ""
		}
		return strings.Join(parts, "\n\n")
	}
	return ""
}
func TransformAnthropic(body []byte, start bool, p Provider, id Identity, session string, now time.Time) ([]byte, error) {
	var obj map[string]any
	if json.Unmarshal(body, &obj) != nil {
		return nil, &Error{Kind: ErrFormat}
	}
	metadata := map[string]any{}
	if old, ok := obj["metadata"].(map[string]any); ok {
		for k, v := range old {
			metadata[k] = v
		}
	}
	user := map[string]string{"account_uuid": "", "session_id": session}
	if id.DeviceMid != "" {
		user["device_id"] = id.DeviceMid
	}
	encoded, _ := json.Marshal(user)
	metadata["user_id"] = string(encoded)
	obj["metadata"] = metadata
	if start {
		var data struct {
			Prefix  string   `json:"cliPrefix"`
			Stable  []string `json:"stableSections"`
			Current struct {
				Line string `json:"line"`
			} `json:"currentDate"`
			Reminder struct {
				Open  string `json:"open"`
				Close string `json:"close"`
			} `json:"systemReminder"`
		}
		if json.Unmarshal(systemData, &data) != nil {
			return nil, &Error{Kind: ErrConfiguration}
		}
		ephemeral := func(text string) map[string]any {
			return map[string]any{"type": "text", "text": text, "cache_control": map[string]string{"type": "ephemeral"}}
		}
		// 3.17.1：block1 使用新的 cliPrefix；客户端 system 存在时以 "\n\n" 前缀
		// 整段替换 block3，未提供时 block3 为空。block2 保持字节精确的 stable 文本。
		stable := strings.Join(data.Stable, "\n\n")
		dynamic := ""
		if custom := customSystemText(obj["system"]); custom != "" {
			dynamic = "\n\n" + custom
		}
		obj["system"] = []any{
			map[string]any{"type": "text", "text": data.Prefix},
			ephemeral(stable),
			ephemeral(dynamic),
		}
		if messages, ok := obj["messages"].([]any); ok && len(messages) > 0 {
			text := data.Reminder.Open + strings.ReplaceAll(data.Current.Line, "{date}", now.Format("2006-01-02")) + data.Reminder.Close
			prefix := map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": text}}}
			obj["messages"] = append([]any{prefix}, messages...)
		}
		if tools, ok := obj["tools"].([]any); ok {
			for _, tool := range tools {
				if t, ok := tool.(map[string]any); ok {
					delete(t, "cache_control")
				}
			}
		}
	}
	if messages, ok := obj["messages"].([]any); ok {
		for _, msg := range messages {
			m, ok := msg.(map[string]any)
			if !ok || m["role"] == "system" {
				continue
			}
			if blocks, ok := m["content"].([]any); ok {
				for _, v := range blocks {
					if b, ok := v.(map[string]any); ok {
						delete(b, "cache_control")
					}
				}
			}
		}
		for i := len(messages) - 1; i >= 0; i-- {
			m, ok := messages[i].(map[string]any)
			if !ok || m["role"] == "system" {
				continue
			}
			switch content := m["content"].(type) {
			case string:
				m["content"] = []any{map[string]any{"type": "text", "text": content, "cache_control": map[string]string{"type": "ephemeral"}}}
			case []any:
				if len(content) > 0 {
					if block, ok := content[len(content)-1].(map[string]any); ok {
						block["cache_control"] = map[string]string{"type": "ephemeral"}
					}
				}
			}
			break
		}
	}
	return json.Marshal(obj)
}
