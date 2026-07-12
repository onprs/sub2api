package ir

// NormalizeSystemInstruction moves system and developer messages into the
// dedicated instruction field while preserving their relative content order.
// Standard targets without distinct conversation roles consume this form.
func NormalizeSystemInstruction(request *Request) {
	if request == nil {
		return
	}
	messages := request.Messages[:0]
	for _, message := range request.Messages {
		if message.Role == RoleSystem || message.Role == RoleDeveloper {
			for _, part := range message.Content {
				if part.Type == ContentText {
					request.SystemInstruction = append(request.SystemInstruction, part)
				}
			}
			continue
		}
		messages = append(messages, message)
	}
	request.Messages = messages
}
