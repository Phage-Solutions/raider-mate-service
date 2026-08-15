package api

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWriteJSONEncodesWithTheGivenStatus(t *testing.T) {
	w := httptest.NewRecorder()
	writeJSON(w, testLogger(), 201, map[string]string{"ok": "yes"})

	if w.Code != 201 {
		t.Errorf("status = %d, want 201", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("content-type = %q, want application/json", ct)
	}

	var got map[string]string
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if got["ok"] != "yes" {
		t.Errorf("body = %v, want {ok: yes}", got)
	}
}

func TestWriteErrorWrapsTheMessage(t *testing.T) {
	w := httptest.NewRecorder()
	writeError(w, testLogger(), 400, "bad request")

	if w.Code != 400 {
		t.Errorf("status = %d, want 400", w.Code)
	}

	var got apiError
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if got.Error != "bad request" {
		t.Errorf("error = %q, want %q", got.Error, "bad request")
	}
}

func TestDecodeJSONDecodesTheBody(t *testing.T) {
	r := httptest.NewRequest("POST", "/", bytes.NewBufferString(`{"name":"Danthrax"}`))

	var body struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(r, &body); err != nil {
		t.Fatalf("decodeJSON: %v", err)
	}
	if body.Name != "Danthrax" {
		t.Errorf("name = %q, want Danthrax", body.Name)
	}
}

func TestDecodeJSONReturnsAnErrorOnMalformedBody(t *testing.T) {
	r := httptest.NewRequest("POST", "/", strings.NewReader(`{not json`))

	var body map[string]string
	if err := decodeJSON(r, &body); err == nil {
		t.Fatalf("decodeJSON = nil error, want one for malformed JSON")
	}
}
