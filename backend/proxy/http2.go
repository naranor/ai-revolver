package proxy

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"time"

	"ai-proxy/config"
	"ai-proxy/logger"
)

var (
	httpClient          *http.Client
	streamClient        *http.Client
	respTimeoutDuration time.Duration
)

func init() {
	// Initialize with default values
	InitHTTPClients(1000, 100, 90*time.Second, 5*time.Second, 300*time.Second)
}

// InitHTTPClients initializes the global HTTP clients for standard and streaming requests
func InitHTTPClients(maxIdleConns, maxIdleConnsPerHost int, idleConnTimeout, connectTimeout, respTimeout time.Duration) {
	respTimeoutDuration = respTimeout

	dialer := &net.Dialer{
		Timeout:   connectTimeout,
		KeepAlive: 30 * time.Second,
	}

	httpTransport := &http.Transport{
		DialContext:         dialer.DialContext,
		TLSHandshakeTimeout: connectTimeout,
		MaxIdleConns:        maxIdleConns,
		MaxIdleConnsPerHost: maxIdleConnsPerHost,
		IdleConnTimeout:     idleConnTimeout,
		ForceAttemptHTTP2:   true,
		TLSClientConfig: &tls.Config{
			MinVersion:         tls.VersionTLS12,
			MaxVersion:         tls.VersionTLS13,
			ClientSessionCache: tls.NewLRUClientSessionCache(100),
		},
	}

	httpClient = &http.Client{
		Transport: httpTransport,
	}

	streamClient = &http.Client{
		Transport: httpTransport,
	}

	logger.Info().
		Int("max_idle_conns", maxIdleConns).
		Int("max_idle_conns_per_host", maxIdleConnsPerHost).
		Dur("idle_conn_timeout", idleConnTimeout).
		Dur("connect_timeout", connectTimeout).
		Dur("resp_timeout", respTimeout).
		Msg("HTTP/2 clients initialized with configurable timeouts and TLS session resumption")
}

// GetResponseTimeout returns the configured response timeout, defaulting to 300s if not set
func GetResponseTimeout() time.Duration {
	if respTimeoutDuration <= 0 {
		return 300 * time.Second
	}
	return respTimeoutDuration
}

func warmHTTP2Connections() {
	cfg := config.GetConfig()
	for _, p := range cfg.Providers {
		if !p.IsEnabled() {
			continue
		}
		url := p.BaseURL
		if url == "" {
			continue
		}
		// Use background context for background health checks
		req, err := http.NewRequestWithContext(context.Background(), "GET", url, nil)
		if err != nil {
			continue
		}
		resp, err := httpClient.Do(req)
		if err != nil {
			continue
		}
		_ = resp.Body.Close()
	}
}
