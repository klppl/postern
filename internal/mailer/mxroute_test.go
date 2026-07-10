package mailer

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func mxServer(t *testing.T, handler func(req mxRouteRequest) (int, mxRouteResponse)) (*httptest.Server, *[]mxRouteRequest) {
	t.Helper()
	var got []mxRouteRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req mxRouteRequest
		_ = json.Unmarshal(body, &req)
		got = append(got, req)
		status, resp := handler(req)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return srv, &got
}

func TestSendMXRouteSuccess(t *testing.T) {
	srv, got := mxServer(t, func(mxRouteRequest) (int, mxRouteResponse) {
		return 200, mxRouteResponse{Success: true, Message: "Email sent successfully."}
	})
	cfg := MXRouteConfig{Server: "tuesday.mxrouting.net", Username: "u@d.com", Password: "pw", Endpoint: srv.URL}
	res, err := SendMXRoute(context.Background(), cfg, &Message{
		From: "u@d.com", To: []string{"a@x.com"}, Subject: "hi", BodyText: "yo",
	})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if res == nil || res.Response == "" {
		t.Fatalf("expected a response, got %+v", res)
	}
	if len(*got) != 1 {
		t.Fatalf("expected 1 request, got %d", len(*got))
	}
	if (*got)[0].To != "a@x.com" || (*got)[0].Server != "tuesday.mxrouting.net" {
		t.Fatalf("unexpected request payload: %+v", (*got)[0])
	}
}

func TestSendMXRouteFansOutPerRecipient(t *testing.T) {
	srv, got := mxServer(t, func(mxRouteRequest) (int, mxRouteResponse) {
		return 200, mxRouteResponse{Success: true, Message: "ok"}
	})
	cfg := MXRouteConfig{Server: "s", Endpoint: srv.URL}
	_, err := SendMXRoute(context.Background(), cfg, &Message{
		From: "u@d.com", To: []string{"a@x.com"}, Cc: []string{"b@x.com"}, Bcc: []string{"c@x.com"},
		Subject: "hi", BodyHTML: "<b>hi</b>",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(*got) != 3 {
		t.Fatalf("expected 3 requests (one per recipient), got %d", len(*got))
	}
	// HTML body preferred over text.
	if (*got)[0].Body != "<b>hi</b>" {
		t.Fatalf("expected HTML body, got %q", (*got)[0].Body)
	}
}

func TestSendMXRouteAuthFailureIsPermanent(t *testing.T) {
	srv, _ := mxServer(t, func(mxRouteRequest) (int, mxRouteResponse) {
		return 200, mxRouteResponse{Success: false, Message: "Authentication failed"}
	})
	cfg := MXRouteConfig{Server: "s", Endpoint: srv.URL}
	_, err := SendMXRoute(context.Background(), cfg, &Message{From: "u@d.com", To: []string{"a@x.com"}, Subject: "s", BodyText: "b"})
	var se *SendError
	if !errors.As(err, &se) {
		t.Fatalf("expected *SendError, got %v", err)
	}
	if !se.Permanent {
		t.Fatalf("auth failure should be permanent, got transient: %+v", se)
	}
}

func TestSendMXRouteRateLimitIsTransient(t *testing.T) {
	srv, _ := mxServer(t, func(mxRouteRequest) (int, mxRouteResponse) {
		return http.StatusTooManyRequests, mxRouteResponse{Success: false, Message: "rate limit exceeded"}
	})
	cfg := MXRouteConfig{Server: "s", Endpoint: srv.URL}
	_, err := SendMXRoute(context.Background(), cfg, &Message{From: "u@d.com", To: []string{"a@x.com"}, Subject: "s", BodyText: "b"})
	var se *SendError
	if !errors.As(err, &se) {
		t.Fatalf("expected *SendError, got %v", err)
	}
	if se.Permanent {
		t.Fatalf("rate limit should be transient, got permanent: %+v", se)
	}
}

func TestSendMXRouteNotConfigured(t *testing.T) {
	_, err := SendMXRoute(context.Background(), MXRouteConfig{}, &Message{From: "u@d.com", To: []string{"a@x.com"}})
	var se *SendError
	if !errors.As(err, &se) || !se.Permanent {
		t.Fatalf("expected permanent SendError for empty server, got %v", err)
	}
}
