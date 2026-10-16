package destination

import (
	"context"
	"errors"
	"hookrelay/internal/core"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestSenderSignatureAndRawBytes(t *testing.T) {
	body := []byte(`{"raw":  1}`)
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		got, err := io.ReadAll(r.Body)
		if err != nil || string(got) != string(body) {
			t.Fatal("payload changed", err)
		}
		if r.Method != "POST" || r.Header.Get("X-Webhook-Id") != "event" || r.Header.Get("X-Webhook-Delivery-Id") != "delivery" || r.Header.Get("X-Webhook-Signature") != Signature("secret", "123", body) {
			t.Fatal("outbound headers", r.Header)
		}
		return &http.Response{StatusCode: 503, Header: http.Header{"Retry-After": []string{"10"}}, Body: io.NopCloser(strings.NewReader("unavailable"))}, nil
	})}
	sender := Sender{Lookup: func(string) (Runtime, bool) {
		return Runtime{URL: "https://example.com", Secret: "secret", Client: client}, true
	}}
	got, err := sender.Send(context.Background(), core.Outbound{EventID: "event", DeliveryID: "delivery", EndpointID: "endpoint", Timestamp: "123", Body: body})
	if err != nil || got.Code != 503 || got.RetryAfter != "10" || got.Error != "" {
		t.Fatal(got, err)
	}
}
func TestSenderSanitizesTransportFailure(t *testing.T) {
	sender := Sender{Lookup: func(string) (Runtime, bool) {
		return Runtime{URL: "https://example.com", Client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("sensitive transport details") })}}, true
	}}
	got, err := sender.Send(context.Background(), core.Outbound{})
	if err != nil || got.Code != 0 || got.Error != "transport failure" {
		t.Fatal(got, err)
	}
}
