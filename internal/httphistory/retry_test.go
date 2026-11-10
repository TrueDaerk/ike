package httphistory

import (
	"reflect"
	"testing"
	"time"

	"ike/internal/httpclient"
)

// A `# @retry` series (#3116) is stored as its final attempt plus the earlier
// outcomes, so a browsed entry shows the same "attempt 3/4" indicator.
func TestRetryAttemptsRoundTrip(t *testing.T) {
	resp := &httpclient.Response{
		Status: "200 OK", StatusCode: 200, Proto: "HTTP/1.1", Body: []byte("ok"),
		Duration: 20 * time.Millisecond, RequestKey: "flaky",
		Attempts: []httpclient.Attempt{
			{Status: "503 Service Unavailable", StatusCode: 503, Duration: 12 * time.Millisecond},
			{Err: "connection refused", Duration: time.Millisecond},
		},
		MaxAttempts: 4,
	}
	s := New(t.TempDir())
	s.Append("req.http", "flaky", FromResponse(resp, time.Now()))
	back := s.List("req.http", "flaky")[0].Response("flaky")
	if !reflect.DeepEqual(back.Attempts, resp.Attempts) || back.MaxAttempts != 4 {
		t.Fatalf("attempts = %+v / %d, want %+v / 4", back.Attempts, back.MaxAttempts, resp.Attempts)
	}
	if back.AttemptLabel() != "attempt 3/4" {
		t.Errorf("label = %q", back.AttemptLabel())
	}
}
