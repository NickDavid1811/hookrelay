package destination

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hookrelay/internal/core"
	"io"
	"net/http"
	"time"
)

// MaxResponseDrainBytes specifies the maximum number of response bytes (4 KiB)
// read and discarded from the destination to enable connection reuse without memory exhaustion.
const MaxResponseDrainBytes int64 = 4096

// Runtime encapsulates the destination URL, cryptographic secret, and HTTP client.
type Runtime struct {
	URL, Secret string
	Client      *http.Client
}

// Sender executes outbound HTTP dispatches against configured runtime endpoints.
type Sender struct {
	Lookup  func(string) (Runtime, bool)
	Resolve func(context.Context, string) (Runtime, bool, error)
}

var _ core.Sender = Sender{}

// Signature computes an HMAC-SHA256 hexadecimal digest over timestamp + "." + body.
func Signature(secret, stamp string, body []byte) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(stamp + "."))
	m.Write(body)
	return hex.EncodeToString(m.Sum(nil))
}
func (s Sender) Send(ctx context.Context, c core.Outbound) (core.HTTPResult, error) {
	var endpoint Runtime
	var exists bool
	if s.Resolve != nil {
		var err error
		endpoint, exists, err = s.Resolve(ctx, c.EndpointID)
		if err != nil {
			return core.HTTPResult{}, err
		}
	} else if s.Lookup != nil {
		endpoint, exists = s.Lookup(c.EndpointID)
	}
	if !exists {
		return core.HTTPResult{}, fmt.Errorf("endpoint configuration missing")
	}
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint.URL, bytes.NewReader(c.Body))
	if err != nil {
		return core.HTTPResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Webhook-Id", c.EventID)
	req.Header.Set("X-Webhook-Delivery-Id", c.DeliveryID)
	req.Header.Set("X-Webhook-Timestamp", c.Timestamp)
	req.Header.Set("X-Webhook-Signature", Signature(endpoint.Secret, c.Timestamp, c.Body))
	result := core.HTTPResult{}
	start := time.Now()
	response, err := endpoint.Client.Do(req)
	if err != nil {
		result.Error = "transport failure"
		result.Err = err
	} else {
		result.Code = response.StatusCode
		if values := response.Header.Values("Retry-After"); len(values) == 1 {
			result.RetryAfter = values[0]
		}
		_, readErr := io.Copy(io.Discard, io.LimitReader(response.Body, MaxResponseDrainBytes))
		response.Body.Close()
		if readErr != nil {
			result.Error = "response read failure"
			result.Err = readErr
		}
	}
	result.Duration = time.Since(start)
	return result, nil
}
