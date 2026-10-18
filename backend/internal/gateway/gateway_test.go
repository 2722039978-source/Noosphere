package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestGateway(t *testing.T) *Gateway {
	t.Helper()
	return New(t.TempDir() + "/models.yaml")
}

func TestHandleModelsListEmpty(t *testing.T) {
	gw := newTestGateway(t)
	req := httptest.NewRequest(http.MethodGet, "/api/gateway/models", nil)
	w := httptest.NewRecorder()

	gw.HandleListModels(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if resp["count"].(float64) != 0 {
		t.Errorf("expected count 0, got %v", resp["count"])
	}
}

func TestHandleModelsAddAndList(t *testing.T) {
	gw := newTestGateway(t)

	body := `{"id":"test-model","name":"Test","provider":"openai","base_url":"https://api.openai.com/v1","api_key":"sk-test","model_name":"gpt-4","is_default":true}`
	req := httptest.NewRequest(http.MethodPost, "/api/gateway/models", strings.NewReader(body))
	w := httptest.NewRecorder()
	gw.HandleAddModel(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 on add, got %d", w.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/gateway/models", nil)
	w = httptest.NewRecorder()
	gw.HandleListModels(w, req)

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["count"].(float64) != 1 {
		t.Errorf("expected count 1 after add, got %v", resp["count"])
	}
}

func TestHandleModelsDelete(t *testing.T) {
	gw := newTestGateway(t)

	body := `{"id":"test-model","name":"Test","provider":"openai","base_url":"https://api.openai.com/v1","api_key":"sk-test","model_name":"gpt-4"}`
	req := httptest.NewRequest(http.MethodPost, "/api/gateway/models", strings.NewReader(body))
	gw.HandleAddModel(httptest.NewRecorder(), req)

	req = httptest.NewRequest(http.MethodDelete, "/api/gateway/models/test-model", nil)
	req.SetPathValue("id", "test-model")
	w := httptest.NewRecorder()
	gw.HandleDeleteModel(w, req)

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["deleted"] != "test-model" {
		t.Errorf("expected deleted=test-model, got %v", resp["deleted"])
	}

	req = httptest.NewRequest(http.MethodGet, "/api/gateway/models", nil)
	w = httptest.NewRecorder()
	gw.HandleListModels(w, req)

	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["count"].(float64) != 0 {
		t.Errorf("expected count 0 after delete, got %v", resp["count"])
	}
}

func TestHandleChatNoModel(t *testing.T) {
	gw := newTestGateway(t)
	body := `{"messages":[{"role":"user","content":"hello"}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/gateway/chat", strings.NewReader(body))
	w := httptest.NewRecorder()

	gw.HandleChat(w, req)

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["error"] == nil {
		t.Error("expected error when no model configured")
	}
}

func TestHandleChatInvalidBody(t *testing.T) {
	gw := newTestGateway(t)
	req := httptest.NewRequest(http.MethodPost, "/api/gateway/chat", strings.NewReader("not json"))
	w := httptest.NewRecorder()

	gw.HandleChat(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid body, got %d", w.Code)
	}
}

func TestHandleTestModelNotFound(t *testing.T) {
	gw := newTestGateway(t)
	req := httptest.NewRequest(http.MethodPost, "/api/gateway/models/nonexistent/test", nil)
	req.SetPathValue("id", "nonexistent")
	w := httptest.NewRecorder()

	gw.HandleTestModel(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for nonexistent model test, got %d", w.Code)
	}
}
