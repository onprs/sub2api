package service

import (
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	protocoltransport "github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/transport"
)

// openAIProtocolOutput receives successful provider output after OpenAI policy
// processing but before client-protocol rendering. A nil output preserves the
// native OpenAI Responses writer path.
type openAIProtocolOutput interface {
	WriteResponse(protocoltransport.Response) error
	WriteStreamHeaders(status int, headers http.Header, actual protocolconv.Protocol) error
	WriteStreamEvent(actual protocolconv.Protocol, payload []byte) error
	FinalizeStream(actual protocolconv.Protocol) error
	ClientOutputStarted() bool
	ClientDisconnected() bool
}
