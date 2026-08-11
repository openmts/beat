package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestJSONResponse(t *testing.T) {
	w := httptest.NewRecorder()

	type testData struct {
		Key string `json:"key"`
	}

	JSONResponse(w, http.StatusOK, &testData{Key: "value"})

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp Response
	_ = json.NewDecoder(w.Body).Decode(&resp)

	if resp.Code != http.StatusOK {
		t.Errorf("expected code 200, got %d", resp.Code)
	}
	if resp.Message != "success" {
		t.Errorf("expected message 'success', got %q", resp.Message)
	}

	dataBytes, _ := json.Marshal(resp.Data)
	var data testData
	_ = json.Unmarshal(dataBytes, &data)
	if data.Key != "value" {
		t.Errorf("expected data.Key 'value', got %q", data.Key)
	}
}

func TestJSONError(t *testing.T) {
	w := httptest.NewRecorder()

	JSONError(w, http.StatusBadRequest, "bad input")

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}

	var resp Response
	_ = json.NewDecoder(w.Body).Decode(&resp)

	if resp.Code != http.StatusBadRequest {
		t.Errorf("expected code 400, got %d", resp.Code)
	}
	if resp.Message != "bad input" {
		t.Errorf("expected message 'bad input', got %q", resp.Message)
	}
	if resp.Data != nil {
		t.Errorf("expected nil data, got %v", resp.Data)
	}
}

func TestParseJSON(t *testing.T) {
	t.Run("valid JSON", func(t *testing.T) {
		body := `{"name": "test", "value": 42}`
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		var result struct {
			Name  string `json:"name"`
			Value int    `json:"value"`
		}

		err := ParseJSON(req, &result)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Name != "test" {
			t.Errorf("expected name 'test', got %q", result.Name)
		}
		if result.Value != 42 {
			t.Errorf("expected value 42, got %d", result.Value)
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		body := `not-json`
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))

		var result struct {
			Name string `json:"name"`
		}

		err := ParseJSON(req, &result)
		if err == nil {
			t.Fatal("expected error for invalid JSON")
		}
	})
}
