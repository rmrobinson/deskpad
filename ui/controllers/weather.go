package controllers

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	weatherv1 "github.com/rmrobinson/weather-server/proto/weather/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
)

const maxBackoff = 30 * time.Second

// Weather maintains a live stream of readings from the weather-server gRPC service.
type Weather struct {
	addr   string
	useTLS bool
	caCert string

	mu      sync.RWMutex
	reading *weatherv1.WeatherReading
}

// NewWeather creates a Weather controller that will stream from addr.
// Set useTLS to enable TLS; set caCert to the path of a PEM CA certificate to
// override the system roots (leave empty to use system roots).
func NewWeather(addr string, useTLS bool, caCert string) *Weather {
	return &Weather{addr: addr, useTLS: useTLS, caCert: caCert}
}

// LatestReading returns the most recently received WeatherReading, or nil if none yet.
func (w *Weather) LatestReading() *weatherv1.WeatherReading {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.reading
}

// Run opens a StreamReadings call and keeps it alive until ctx is cancelled,
// reconnecting with exponential backoff on any error.
func (w *Weather) Run(ctx context.Context) {
	backoff := time.Second
	for {
		if ctx.Err() != nil {
			return
		}
		wasConnected, err := w.runStream(ctx)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			log.Printf("[weather] stream: %v; reconnect in %s", err, backoff)
		}
		if wasConnected {
			backoff = time.Second
		} else if backoff < maxBackoff {
			backoff *= 2
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
	}
}

// runStream opens one StreamReadings call and processes messages until an error
// or context cancellation. Returns (wasConnected, nil) on ctx cancellation,
// (wasConnected, err) on stream failure; wasConnected is true if at least one
// message was received before the failure.
func (w *Weather) runStream(ctx context.Context) (wasConnected bool, _ error) {
	opts, err := w.dialOpts()
	if err != nil {
		return false, err
	}
	conn, err := grpc.NewClient(w.addr, opts...)
	if err != nil {
		return false, fmt.Errorf("dial %s: %w", w.addr, err)
	}
	defer conn.Close()

	client := weatherv1.NewWeatherServiceClient(conn)
	stream, err := client.StreamReadings(ctx, &weatherv1.StreamRequest{})
	if err != nil {
		if ctx.Err() != nil {
			return false, nil
		}
		return false, fmt.Errorf("open stream: %w", err)
	}

	log.Printf("[weather] stream connected to %s\n", w.addr)
	for {
		reading, err := stream.Recv()
		if err != nil {
			if ctx.Err() != nil {
				return wasConnected, nil
			}
			return wasConnected, fmt.Errorf("recv: %w", err)
		}
		wasConnected = true

		w.mu.Lock()
		w.reading = reading
		w.mu.Unlock()
	}
}

func (w *Weather) dialOpts() ([]grpc.DialOption, error) {
	var credOpt grpc.DialOption
	if !w.useTLS {
		credOpt = grpc.WithTransportCredentials(insecure.NewCredentials())
	} else {
		tlsCfg := &tls.Config{}
		if w.caCert != "" {
			pem, err := os.ReadFile(w.caCert)
			if err != nil {
				return nil, fmt.Errorf("read ca_cert %s: %w", w.caCert, err)
			}
			pool := x509.NewCertPool()
			if !pool.AppendCertsFromPEM(pem) {
				return nil, fmt.Errorf("ca_cert %s: no valid PEM certificates found", w.caCert)
			}
			tlsCfg.RootCAs = pool
		}
		credOpt = grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg))
	}
	kaOpt := grpc.WithKeepaliveParams(keepalive.ClientParameters{
		Time:                30 * time.Second,
		Timeout:             10 * time.Second,
		PermitWithoutStream: true,
	})
	return []grpc.DialOption{credOpt, kaOpt}, nil
}
