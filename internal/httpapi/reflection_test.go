package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestHandleGetIP(t *testing.T) {
	router, _ := newTestRouter()

	recorder := doRequest(router, http.MethodGet, "/reflection/ip", "203.0.113.4:51234", "")

	assertStatus(t, recorder, http.StatusOK)

	var body struct {
		IP string `json:"ip"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("response body is not a JSON object: %v (body: %s)", err, recorder.Body.String())
	}

	// The caller's ephemeral port is deliberately not echoed back.
	if body.IP != "203.0.113.4" {
		t.Errorf("ip = %q, want %q", body.IP, "203.0.113.4")
	}
}

func TestHandleGetIPMalformedRemoteAddr(t *testing.T) {
	router, _ := newTestRouter()

	recorder := doRequest(router, http.MethodGet, "/reflection/ip", "203.0.113.4", "")

	assertStatus(t, recorder, http.StatusInternalServerError)
	assertResult(t, recorder, "internal server error")
}
