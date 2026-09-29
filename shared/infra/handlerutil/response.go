package handlerutil

import (
	"encoding/json"
	"log"
	"net/http"
)

// WriteJSON encodes data as JSON and writes it to the response writer, handling any errors.
func WriteJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("[ERROR] failed to encode response: %v", err)
	}
}

// WriteBytes writes raw bytes to the response writer, handling any errors.
func WriteBytes(w http.ResponseWriter, data []byte) {
	if _, err := w.Write(data); err != nil {
		log.Printf("[ERROR] failed to write response: %v", err)
	}
}
