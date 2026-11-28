package providers

import (
	"encoding/json"
	"testing"
)

func TestToolCheckpointPreservesBothTransports(t *testing.T) {
	m := ToolMessage{Role: "assistant", ToolCalls: []ToolCall{{ID: "call", ExtraContent: map[string]json.RawMessage{"signature": json.RawMessage(`"opaque"`)}}}, responsesOutput: []json.RawMessage{json.RawMessage(`{"type":"reasoning","opaque":"abc"}`)}}
	b, err := json.Marshal(CheckpointToolMessage(m))
	if err != nil {
		t.Fatal(err)
	}
	var r ToolHistory
	if err = json.Unmarshal(b, &r); err != nil {
		t.Fatal(err)
	}
	got := r.Restore()
	if string(got.ToolCalls[0].ExtraContent["signature"]) != `"opaque"` || string(got.responsesOutput[0]) != string(m.responsesOutput[0]) {
		t.Fatal(got)
	}
}
