package service

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

// ClinePassError is the normalized, credential-safe error contract used by
// gateway failover, account tests, and usage refreshes.
type ClinePassError struct {
	HTTPStatus       int
	ProviderStatus   int
	Type             string
	Code             string
	Message          string
	RequestID        string
	Retryable        bool
	AccountAffecting bool
	ClientError      bool
}

func (e *ClinePassError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	return http.StatusText(e.EffectiveStatus())
}

func (e *ClinePassError) EffectiveStatus() int {
	if e != nil && e.ProviderStatus >= 100 && e.ProviderStatus <= 599 {
		return e.ProviderStatus
	}
	if e == nil {
		return 0
	}
	return e.HTTPStatus
}

func decodeClinePassError(status int, headers http.Header, body []byte) *ClinePassError {
	result := &ClinePassError{HTTPStatus: status}
	if headers != nil {
		result.RequestID = strings.TrimSpace(headers.Get("x-request-id"))
	}

	var root map[string]json.RawMessage
	if json.Unmarshal(body, &root) == nil {
		applyClinePassErrorObject(result, root)
		if raw, ok := root["error"]; ok {
			var text string
			if json.Unmarshal(raw, &text) == nil {
				text = strings.TrimSpace(text)
				if text != "" {
					result.Message = text
				}
				if nested := decodeFirstJSONObject(text); nested != nil {
					applyClinePassErrorObject(result, nested)
				}
			} else {
				var object map[string]json.RawMessage
				if json.Unmarshal(raw, &object) == nil {
					applyClinePassErrorObject(result, object)
				}
			}
		}
	}

	if strings.TrimSpace(result.Message) == "" {
		result.Message = http.StatusText(status)
	}
	result.Message = sanitizeUpstreamErrorMessage(result.Message)
	classifyClinePassError(result)
	return result
}

func applyClinePassErrorObject(result *ClinePassError, object map[string]json.RawMessage) {
	if result == nil || object == nil {
		return
	}
	if status := clinePassJSONInt(object["status"]); status >= 100 && status <= 599 {
		result.ProviderStatus = status
	}
	if message := clinePassJSONString(object["message"]); message != "" {
		result.Message = message
	}
	if detail := clinePassJSONString(object["detail"]); result.Message == "" && detail != "" {
		result.Message = detail
	}
	if value := clinePassJSONString(object["type"]); value != "" {
		result.Type = value
	}
	if value := clinePassJSONString(object["code"]); value != "" {
		result.Code = value
		if result.ProviderStatus == 0 {
			if parsed, err := strconv.Atoi(value); err == nil && parsed >= 100 && parsed <= 599 {
				result.ProviderStatus = parsed
			}
		}
	} else if value := clinePassJSONInt(object["code"]); value != 0 {
		result.Code = strconv.Itoa(value)
		if result.ProviderStatus == 0 && value >= 100 && value <= 599 {
			result.ProviderStatus = value
		}
	}
	if raw, ok := object["error"]; ok {
		var nested map[string]json.RawMessage
		if json.Unmarshal(raw, &nested) == nil {
			applyClinePassErrorObject(result, nested)
		}
	}
}

func decodeFirstJSONObject(text string) map[string]json.RawMessage {
	for index := strings.IndexByte(text, '{'); index >= 0 && index < len(text); {
		decoder := json.NewDecoder(bytes.NewBufferString(text[index:]))
		var object map[string]json.RawMessage
		if decoder.Decode(&object) == nil && object != nil {
			return object
		}
		next := strings.IndexByte(text[index+1:], '{')
		if next < 0 {
			break
		}
		index += next + 1
	}
	return nil
}

func classifyClinePassError(result *ClinePassError) {
	if result == nil {
		return
	}
	effectiveStatus := result.EffectiveStatus()
	typeValue := strings.ToLower(strings.TrimSpace(result.Type))
	codeValue := strings.ToLower(strings.TrimSpace(result.Code))
	message := strings.ToLower(strings.TrimSpace(result.Message))

	result.ClientError = effectiveStatus == http.StatusBadRequest ||
		effectiveStatus == http.StatusNotFound ||
		typeValue == "invalid_request_error" || typeValue == "invalid_parameter_error" ||
		codeValue == "invalid_request_error" || codeValue == "invalid_parameter_error" ||
		(effectiveStatus == http.StatusNotFound && strings.Contains(message, "model"))
	if result.ClientError {
		result.Retryable = false
		result.AccountAffecting = false
		return
	}

	switch effectiveStatus {
	case http.StatusUnauthorized, http.StatusForbidden:
		result.AccountAffecting = true
	case http.StatusTooManyRequests:
		result.Retryable = true
	default:
		result.Retryable = effectiveStatus >= http.StatusInternalServerError || effectiveStatus == 0
	}
}

func clinePassJSONString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var value string
	if json.Unmarshal(raw, &value) == nil {
		return strings.TrimSpace(value)
	}
	return ""
}

func clinePassJSONInt(raw json.RawMessage) int {
	if len(raw) == 0 {
		return 0
	}
	var number json.Number
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&number) == nil {
		value, _ := strconv.Atoi(number.String())
		return value
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		value, _ := strconv.Atoi(strings.TrimSpace(text))
		return value
	}
	return 0
}
