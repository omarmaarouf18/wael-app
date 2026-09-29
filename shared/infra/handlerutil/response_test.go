package handlerutil

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	payload := map[string]string{"status": "ok", "message": "success"}

	WriteJSON(rec, http.StatusCreated, payload)

	if rec.Code != http.StatusCreated {
		t.Errorf("Expected status %d, got %d", http.StatusCreated, rec.Code)
	}

	if contentType := rec.Header().Get("Content-Type"); contentType != "application/json" {
		t.Errorf("Expected Content-Type application/json, got %q", contentType)
	}

	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to unmarshal JSON response: %v", err)
	}

	if resp["status"] != "ok" || resp["message"] != "success" {
		t.Errorf("Unexpected body payload: %v", resp)
	}
}

func TestWriteBytes(t *testing.T) {
	rec := httptest.NewRecorder()
	data := []byte("hello raw bytes")

	WriteBytes(rec, data)

	if rec.Body.String() != "hello raw bytes" {
		t.Errorf("Expected 'hello raw bytes', got %q", rec.Body.String())
	}
}
