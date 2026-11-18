package apierr

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestWrite(t *testing.T) {
	rec := httptest.NewRecorder()
	Write(rec, 429, RateLimit, "rate_limit_exceeded", "slow down")
	var got struct {
		Error struct{ Message, Type, Code string } `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if rec.Code != 429 || got.Error.Message != "slow down" || got.Error.Type != RateLimit || got.Error.Code != "rate_limit_exceeded" {
		t.Fatalf("unexpected response: %d %s", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q", ct)
	}
}
