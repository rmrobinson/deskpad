package main

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"

	"github.com/spf13/viper"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// bridgeServerOptions builds the gRPC server options for the house bridge
// listener from the bridge.* config keys. With bridge.use-tls unset or
// false, the server accepts plaintext connections - appropriate for a
// bridge reachable only on a trusted local network. When true, it requires
// bridge.cert-file/bridge.key-file for its server certificate; if
// bridge.ca-cert is also set, it additionally requires and verifies a
// client certificate signed by that CA (mutual TLS), mirroring the
// credential shape the House controller's client side already supports.
func bridgeServerOptions() ([]grpc.ServerOption, error) {
	if !viper.GetBool("bridge.use-tls") {
		return nil, nil
	}

	certFile := viper.GetString("bridge.cert-file")
	keyFile := viper.GetString("bridge.key-file")
	if certFile == "" || keyFile == "" {
		return nil, fmt.Errorf("bridge.use-tls is set but bridge.cert-file/bridge.key-file are not")
	}

	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("load bridge cert/key: %w", err)
	}
	tlsCfg := &tls.Config{Certificates: []tls.Certificate{cert}}

	if caCert := viper.GetString("bridge.ca-cert"); caCert != "" {
		pem, err := os.ReadFile(caCert)
		if err != nil {
			return nil, fmt.Errorf("read bridge.ca-cert %s: %w", caCert, err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("bridge.ca-cert %s: no valid PEM certificates found", caCert)
		}
		tlsCfg.ClientCAs = pool
		tlsCfg.ClientAuth = tls.RequireAndVerifyClientCert
	}

	return []grpc.ServerOption{grpc.Creds(credentials.NewTLS(tlsCfg))}, nil
}
