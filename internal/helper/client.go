package helper

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

type Client struct {
	http      *http.Client
	transport *http.Transport
}

// serverUID must be zero in production. Explicit injection allows integration
// testing with an unprivileged private helper without weakening production.
func NewClient(socket string, serverUID uint32) *Client {
	tr := &http.Transport{MaxConnsPerHost: 4, MaxIdleConns: 2, IdleConnTimeout: 30 * time.Second, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var d net.Dialer
		c, err := d.DialContext(ctx, "unix", socket)
		if err != nil {
			return nil, err
		}
		uid, err := peerUID(c)
		if err != nil || uid != serverUID {
			c.Close()
			return nil, fmt.Errorf("helper peer credentials rejected")
		}
		return c, nil
	}}
	return &Client{http: &http.Client{Transport: tr, Timeout: 110 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, transport: tr}
}
func (c *Client) Close() { c.transport.CloseIdleConnections() }

// Call transports only named methods; the authoritative allowlist is enforced
// at the privileged server. There is intentionally no remote filesystem API.
func (c *Client) Call(ctx context.Context, method string, input, output any) error {
	for _, r := range method {
		if r < 'A' || r > 'Z' {
			if r < 'a' || r > 'z' {
				return fmt.Errorf("invalid RPC name")
			}
		}
	}
	if method == "" {
		return fmt.Errorf("missing RPC name")
	}
	b, err := json.Marshal(input)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", "http://helper/rpc/"+method, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 32<<20))
	if err != nil {
		return err
	}
	if res.StatusCode != 200 {
		var e RPCError
		if json.Unmarshal(body, &e) == nil && e.Code != "" {
			return &e
		}
		return fmt.Errorf("helper RPC returned HTTP %d", res.StatusCode)
	}
	if output == nil {
		return nil
	}
	return json.Unmarshal(body, output)
}
