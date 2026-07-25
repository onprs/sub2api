package openaichat

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/ir"
)

const chatToolContentCloseTag = "</tool-content>"

func extractChatToolResultContent(messages []apicompat.ChatMessage) (map[string][]ir.ContentPart, []apicompat.ChatMessage) {
	packs := make(map[string][]ir.ContentPart)
	clean := make([]apicompat.ChatMessage, 0, len(messages))
	for _, message := range messages {
		sections, ok := decodeChatToolContentMessage(message)
		if !ok {
			clean = append(clean, message)
			continue
		}
		for callID, content := range sections {
			packs[callID] = content
		}
	}
	return packs, clean
}

func decodeChatToolContentMessage(message apicompat.ChatMessage) (map[string][]ir.ContentPart, bool) {
	if message.Role != "user" {
		return nil, false
	}
	var parts []apicompat.ChatContentPart
	if json.Unmarshal(message.Content, &parts) != nil || len(parts) < 3 {
		return nil, false
	}

	sections := make(map[string][]ir.ContentPart)
	for i := 0; i < len(parts); {
		callID, ok := chatToolContentCallID(parts[i])
		if !ok {
			return nil, false
		}
		i++
		var content []ir.ContentPart
		for i < len(parts) && (parts[i].Type != "text" || parts[i].Text != chatToolContentCloseTag) {
			switch parts[i].Type {
			case "text":
				content = append(content, ir.ContentPart{Type: ir.ContentText, Text: parts[i].Text})
			case "image_url":
				if parts[i].ImageURL == nil || strings.TrimSpace(parts[i].ImageURL.URL) == "" {
					return nil, false
				}
				content = append(content, chatImagePart(parts[i].ImageURL.URL))
			default:
				return nil, false
			}
			i++
		}
		if i >= len(parts) || len(content) == 0 {
			return nil, false
		}
		sections[callID] = content
		i++
	}
	return sections, len(sections) > 0
}

func chatToolContentCallID(part apicompat.ChatContentPart) (string, bool) {
	if part.Type != "text" {
		return "", false
	}
	const prefix = `<tool-content call-id="`
	if !strings.HasPrefix(part.Text, prefix) || !strings.HasSuffix(part.Text, `">`) {
		return "", false
	}
	callID := strings.TrimSuffix(strings.TrimPrefix(part.Text, prefix), `">`)
	return callID, callID != ""
}

func restoreChatToolResultContent(request *ir.Request, packs map[string][]ir.ContentPart) {
	if request == nil || len(packs) == 0 {
		return
	}
	for i := range request.Messages {
		for j := range request.Messages[i].Content {
			part := &request.Messages[i].Content[j]
			if part.Type != ir.ContentToolResult {
				continue
			}
			content, ok := packs[part.ToolCallID]
			if !ok {
				continue
			}
			part.ToolResult = nil
			part.ToolResultContent = cloneChatToolResultContent(content)
		}
	}
}

func injectChatToolResultContent(messages []apicompat.ChatMessage, request *ir.Request) []apicompat.ChatMessage {
	packs := chatToolResultContentByCallID(request)
	if len(packs) == 0 {
		return messages
	}

	out := make([]apicompat.ChatMessage, 0, len(messages)+len(packs))
	for i := 0; i < len(messages); {
		message := messages[i]
		out = append(out, message)
		i++
		if message.Role != "tool" {
			continue
		}

		callIDs := []string{message.ToolCallID}
		for i < len(messages) && messages[i].Role == "tool" {
			out = append(out, messages[i])
			callIDs = append(callIDs, messages[i].ToolCallID)
			i++
		}
		parts := make([]apicompat.ChatContentPart, 0)
		for _, callID := range callIDs {
			content, ok := packs[callID]
			if !ok {
				continue
			}
			parts = append(parts, apicompat.ChatContentPart{Type: "text", Text: fmt.Sprintf(`<tool-content call-id="%s">`, callID)})
			parts = append(parts, content...)
			parts = append(parts, apicompat.ChatContentPart{Type: "text", Text: chatToolContentCloseTag})
		}
		if len(parts) > 0 {
			body, _ := json.Marshal(parts)
			out = append(out, apicompat.ChatMessage{Role: "user", Content: body})
		}
	}
	return out
}

func chatToolResultContentByCallID(request *ir.Request) map[string][]apicompat.ChatContentPart {
	packs := make(map[string][]apicompat.ChatContentPart)
	if request == nil {
		return packs
	}
	for _, message := range request.Messages {
		for _, part := range message.Content {
			if part.Type != ir.ContentToolResult || len(part.ToolResultContent) == 0 {
				continue
			}
			var content []apicompat.ChatContentPart
			for _, resultPart := range part.ToolResultContent {
				switch resultPart.Type {
				case ir.ContentText:
					content = append(content, apicompat.ChatContentPart{Type: "text", Text: resultPart.Text})
				case ir.ContentImage:
					content = append(content, apicompat.ChatContentPart{Type: "image_url", ImageURL: &apicompat.ChatImageURL{URL: chatImageURL(resultPart)}})
				}
			}
			if len(content) > 0 {
				packs[part.ToolCallID] = content
			}
		}
	}
	return packs
}

func chatImagePart(value string) ir.ContentPart {
	if strings.HasPrefix(value, "data:") {
		media, data, ok := strings.Cut(strings.TrimPrefix(value, "data:"), ";base64,")
		if ok {
			return ir.ContentPart{Type: ir.ContentImage, MediaType: media, Data: data}
		}
	}
	return ir.ContentPart{Type: ir.ContentImage, URL: value}
}

func chatImageURL(part ir.ContentPart) string {
	if part.URL != "" {
		return part.URL
	}
	return "data:" + part.MediaType + ";base64," + part.Data
}

func cloneChatToolResultContent(content []ir.ContentPart) []ir.ContentPart {
	out := make([]ir.ContentPart, len(content))
	copy(out, content)
	return out
}
