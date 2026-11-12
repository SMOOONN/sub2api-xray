package antigravity

import (
	"context"
	"fmt"
	"net"
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/pkg/proxyurl"
	"github.com/Wei-Shaw/sub2api/internal/pkg/proxyutil"
)

// NewClientWithDialer retains the standard client behavior while allowing its
// caller to enforce a socket policy on both direct and proxy connections.
// A nil dialer preserves NewClient's transport and proxy timeout defaults.
func NewClientWithDialer(proxyURL string, dialContext func(context.Context, string, string) (net.Conn, error)) (*Client, error) {
	client := &http.Client{Timeout: clientTimeout}
	_, parsed, err := proxyurl.Parse(proxyURL)
	if err != nil {
		return nil, err
	}
	if parsed != nil {
		transport := &http.Transport{
			DialContext:         (&net.Dialer{Timeout: proxyDialTimeout}).DialContext,
			TLSHandshakeTimeout: proxyTLSHandshakeTimeout,
		}
		if err := proxyutil.ConfigureTransportProxyWithDialer(transport, parsed, dialContext); err != nil {
			return nil, fmt.Errorf("configure proxy: %w", err)
		}
		if dialContext != nil {
			protectedDial := transport.DialContext
			transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
				ctx, cancel := context.WithTimeout(ctx, proxyDialTimeout)
				defer cancel()
				return protectedDial(ctx, network, address)
			}
		}
		client.Transport = transport
	} else if dialContext != nil {
		base, ok := http.DefaultTransport.(*http.Transport)
		if !ok {
			return nil, fmt.Errorf("default HTTP transport cannot enforce a socket policy")
		}
		transport := base.Clone()
		transport.DialContext = dialContext
		//nolint:staticcheck // SA1019: Clear the legacy TLS hook so HTTPS cannot bypass the injected socket policy.
		transport.DialTLS = nil
		transport.DialTLSContext = nil
		client.Transport = transport
	}
	return &Client{httpClient: client}, nil
}
