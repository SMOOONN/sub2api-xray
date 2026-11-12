package antigravity

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestClientWithDialerGuardsProxyConnections(t *testing.T) {
	for _, scheme := range []string{"http", "https", "socks5", "socks5h"} {
		t.Run(scheme, func(t *testing.T) {
			blocked := errors.New("socket policy denied connection")
			var dialed string
			var dialNetwork string
			var dialDeadline time.Time
			var hasDeadline bool
			client, err := NewClientWithDialer(scheme+"://127.0.0.1:12345", func(ctx context.Context, network, address string) (net.Conn, error) {
				dialed = address
				dialNetwork = network
				dialDeadline, hasDeadline = ctx.Deadline()
				return nil, blocked
			})
			require.NoError(t, err)
			_, err = client.httpClient.Get("https://catalog.example/v1internal:fetchAvailableModels")
			require.ErrorIs(t, err, blocked)
			require.Equal(t, "127.0.0.1:12345", dialed)
			require.Equal(t, "tcp", dialNetwork)
			require.True(t, hasDeadline)
			require.LessOrEqual(t, time.Until(dialDeadline), clientTimeout)
		})
	}
}

type handshakeDeadlineConn struct {
	net.Conn
	firstDeadline atomic.Int64
}

func (c *handshakeDeadlineConn) SetDeadline(deadline time.Time) error {
	if !deadline.IsZero() {
		c.firstDeadline.CompareAndSwap(0, deadline.UnixNano())
	}
	return c.Conn.SetDeadline(deadline)
}

func TestClientWithDialerBoundsSOCKSHandshake(t *testing.T) {
	for _, scheme := range []string{"socks5", "socks5h"} {
		for _, callerTimeout := range []time.Duration{0, time.Second} {
			t.Run(scheme+"/"+callerTimeout.String(), func(t *testing.T) {
				ctx := context.Background()
				if callerTimeout > 0 {
					var cancel context.CancelFunc
					ctx, cancel = context.WithTimeout(ctx, callerTimeout)
					defer cancel()
				}
				clientSide, proxySide := net.Pipe()
				recorded := &handshakeDeadlineConn{Conn: clientSide}
				done := make(chan struct{})
				go func() {
					defer close(done)
					defer func() { _ = proxySide.Close() }()
					_, _ = io.ReadFull(proxySide, make([]byte, 3))
				}()
				t.Cleanup(func() {
					_ = clientSide.Close()
					_ = proxySide.Close()
					<-done
				})
				client, err := NewClientWithDialer(scheme+"://127.0.0.1:12345", func(context.Context, string, string) (net.Conn, error) {
					return recorded, nil
				})
				require.NoError(t, err)
				transport, ok := client.httpClient.Transport.(*http.Transport)
				require.True(t, ok)
				_, err = transport.DialContext(ctx, "tcp", "catalog.example:443")
				require.Error(t, err)
				require.NotZero(t, recorded.firstDeadline.Load(), "the SOCKS handshake must receive a deadline")
				deadline := time.Unix(0, recorded.firstDeadline.Load())
				require.LessOrEqual(t, time.Until(deadline), proxyDialTimeout)
				if callerDeadline, ok := ctx.Deadline(); ok {
					require.False(t, deadline.After(callerDeadline), "the proxy budget must not extend the caller deadline")
				}
			})
		}
	}
}

type unsupportedPolicyTransport struct{}

func (unsupportedPolicyTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("transport without a socket policy")
}

func TestClientWithDialerRejectsUnsupportedDefaultTransport(t *testing.T) {
	original := http.DefaultTransport
	http.DefaultTransport = unsupportedPolicyTransport{}
	t.Cleanup(func() { http.DefaultTransport = original })
	client, err := NewClientWithDialer("", func(context.Context, string, string) (net.Conn, error) {
		return nil, errors.New("guarded")
	})
	require.Error(t, err)
	require.Nil(t, client)
}

func TestClientWithDialerGuardsDirectTLSAndPreservesDefaultTransport(t *testing.T) {
	original := http.DefaultTransport
	base := &http.Transport{
		ForceAttemptHTTP2:   true,
		MaxIdleConns:        37,
		TLSHandshakeTimeout: 9 * time.Second,
	}
	var unguardedCalls int
	base.DialTLSContext = func(context.Context, string, string) (net.Conn, error) {
		unguardedCalls++
		return nil, errors.New("unguarded TLS dial")
	}
	//nolint:staticcheck // SA1019: Exercise the legacy hook too: either TLS hook must not bypass the policy.
	base.DialTLS = func(string, string) (net.Conn, error) {
		unguardedCalls++
		return nil, errors.New("unguarded legacy TLS dial")
	}
	http.DefaultTransport = base
	t.Cleanup(func() { http.DefaultTransport = original })

	blocked := errors.New("socket policy denied direct connection")
	var guardedCalls int
	client, err := NewClientWithDialer("", func(context.Context, string, string) (net.Conn, error) {
		guardedCalls++
		return nil, blocked
	})
	require.NoError(t, err)
	transport, ok := client.httpClient.Transport.(*http.Transport)
	require.True(t, ok)
	require.NotSame(t, base, transport)
	require.True(t, transport.ForceAttemptHTTP2)
	require.Equal(t, base.MaxIdleConns, transport.MaxIdleConns)
	require.Equal(t, base.TLSHandshakeTimeout, transport.TLSHandshakeTimeout)
	_, err = client.httpClient.Get("https://catalog.example/models")
	require.ErrorIs(t, err, blocked)
	require.Positive(t, guardedCalls)
	require.Zero(t, unguardedCalls)
	require.NotNil(t, base.DialTLSContext, "the global transport must remain unchanged")
}

func TestClientWithNilDialerRetainsDefaultTransport(t *testing.T) {
	client, err := NewClientWithDialer("", nil)
	require.NoError(t, err)
	require.Nil(t, client.httpClient.Transport)
	require.Equal(t, clientTimeout, client.httpClient.Timeout)
}
