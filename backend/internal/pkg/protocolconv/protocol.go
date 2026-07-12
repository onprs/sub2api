package protocolconv

import (
	"fmt"
	"strings"
)

// Protocol identifies a standard API wire format. Vendor adapters are not
// protocols and must remain outside this enum.
type Protocol string

const (
	ProtocolOpenAIChat      Protocol = "openai_chat_completions"
	ProtocolOpenAIResponses Protocol = "openai_responses"
	ProtocolAnthropic       Protocol = "anthropic_messages"
	ProtocolGoogleGenAI     Protocol = "google_genai"
)

var standardProtocols = [...]Protocol{
	ProtocolOpenAIChat,
	ProtocolOpenAIResponses,
	ProtocolAnthropic,
	ProtocolGoogleGenAI,
}

// StandardProtocols returns a copy in stable registration order.
func StandardProtocols() []Protocol {
	out := make([]Protocol, len(standardProtocols))
	copy(out, standardProtocols[:])
	return out
}

// Valid reports whether p identifies a supported standard wire protocol.
func (p Protocol) Valid() bool {
	switch p {
	case ProtocolOpenAIChat, ProtocolOpenAIResponses, ProtocolAnthropic, ProtocolGoogleGenAI:
		return true
	default:
		return false
	}
}

// Validate rejects empty and vendor-specific protocol identifiers.
func (p Protocol) Validate() error {
	if p.Valid() {
		return nil
	}
	return &Error{Code: ErrorUnsupportedProtocol, Protocol: p, Message: fmt.Sprintf("unsupported standard protocol %q", p)}
}

func (p Protocol) String() string {
	return string(p)
}

// ParseProtocol parses a configured standard protocol ID. It deliberately does
// not inspect endpoints, request bodies, or model names.
func ParseProtocol(value string) (Protocol, error) {
	p := Protocol(strings.ToLower(strings.TrimSpace(value)))
	if err := p.Validate(); err != nil {
		return "", err
	}
	return p, nil
}
