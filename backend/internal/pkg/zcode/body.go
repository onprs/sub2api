package zcode

// 来源：src/proxy/body-transformer.ts、system-prompt.ts、trace-headers.ts。
// zcode_system.json 是固定上游协议素材；指令文本只作为出站数据，不控制本服务。
import (
	_ "embed"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

//go:embed zcode_system.json
var systemData []byte

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
			Dynamic struct {
				Before string `json:"beforeEnvironment"`
				After  string `json:"afterEnvironment"`
			} `json:"dynamicSections"`
			Env struct {
				Heading  string `json:"heading"`
				Invoked  string `json:"invokedLine"`
				Cwd      string `json:"cwdLabel"`
				Git      string `json:"gitLabel"`
				GitNo    string `json:"gitNo"`
				Platform string `json:"platformLabel"`
				Shell    string `json:"shellLabel"`
				OS       string `json:"osVersionLabel"`
				Powered  string `json:"poweredByLine"`
			} `json:"environment"`
			Context struct {
				Intro   string `json:"intro"`
				Outro   string `json:"outro"`
				Heading string `json:"currentDateHeading"`
				Line    string `json:"currentDateLine"`
			} `json:"contextPrefix"`
		}
		if json.Unmarshal(systemData, &data) != nil {
			return nil, &Error{Kind: ErrConfiguration}
		}
		cwd, _ := os.Getwd()
		shell := os.Getenv("SHELL")
		if shell == "" {
			shell = os.Getenv("ComSpec")
		}
		shell = filepath.Base(shell)
		if shell == "." || shell == "" {
			shell = "unknown"
		}
		platform := strings.Split(id.Platform, "-")[0]
		osVersion := strings.TrimSpace(platform + " " + id.OSVersion + " " + strings.TrimPrefix(id.Platform, platform+"-"))
		providerID := "zai-api"
		if p == BigModel {
			providerID = "bigmodel-api"
		}
		model, _ := obj["model"].(string)
		env := []string{data.Env.Heading, data.Env.Invoked, "- " + data.Env.Cwd + ": " + cwd, "- " + data.Env.Git + ": " + data.Env.GitNo, "- " + data.Env.Platform + ": " + platform, "- " + data.Env.Shell + ": " + shell, "- " + data.Env.OS + ": " + osVersion}
		if model != "" {
			env = append(env, "- "+strings.ReplaceAll(strings.ReplaceAll(data.Env.Powered, "{provider}", providerID), "{model}", model))
		}
		ephemeral := func(text string) map[string]any {
			return map[string]any{"type": "text", "text": text, "cache_control": map[string]string{"type": "ephemeral"}}
		}
		blocks := []any{ephemeral(data.Prefix), ephemeral(strings.Join(data.Stable, "\n\n")), ephemeral("\n\n" + strings.Join([]string{data.Dynamic.Before, strings.Join(env, "\n"), data.Dynamic.After}, "\n\n"))}
		switch existing := obj["system"].(type) {
		case string:
			if strings.TrimSpace(existing) != "" {
				blocks = append(blocks, map[string]any{"type": "text", "text": existing})
			}
		case []any:
			for _, item := range existing {
				if b, ok := item.(map[string]any); ok && b["type"] == "text" {
					if text, ok := b["text"].(string); ok {
						blocks = append(blocks, map[string]any{"type": "text", "text": text})
					}
				}
			}
		}
		obj["system"] = blocks
		if messages, ok := obj["messages"].([]any); ok && len(messages) > 0 {
			text := "<system-reminder>" + strings.Join([]string{data.Context.Intro, data.Context.Heading + "\n" + strings.ReplaceAll(data.Context.Line, "{date}", now.Format("2006-01-02")), "", data.Context.Outro}, "\n") + "</system-reminder>"
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
