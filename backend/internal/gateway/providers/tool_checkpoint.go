package providers

import "encoding/json"

// ToolHistory is the private checkpoint format. Provider state belongs to the
// model transport and must never be projected onto the public event timeline.
type ToolHistory struct {
	Message   ToolMessage       `json:"message"`
	Responses []json.RawMessage `json:"responses,omitempty"`
}

func CheckpointToolMessage(message ToolMessage) ToolHistory {
	return ToolHistory{Message: message, Responses: message.responsesOutput}
}

func (record ToolHistory) Restore() ToolMessage {
	message := record.Message
	message.responsesOutput = record.Responses
	return message
}
