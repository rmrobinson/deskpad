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

	caCert := viper.GetString("bridge.ca-cert")
	clientName := viper.GetString("bridge.client-name")
	if clientName != "" && caCert == "" {
		return nil, fmt.Errorf("bridge.client-name is set but bridge.ca-cert is not")
	}

	if caCert != "" {
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

		if clientName != "" {
			tlsCfg.VerifyPeerCertificate = verifyClientName(clientName)
		}
	}

	return []grpc.ServerOption{grpc.Creds(credentials.NewTLS(tlsCfg))}, nil
}

// verifyClientName returns a tls.Config.VerifyPeerCertificate callback that,
// on top of Go's own chain-of-trust check (tls.RequireAndVerifyClientCert
// against bridge.ca-cert), additionally rejects any client certificate whose
// DNS SAN doesn't include name. This matters here specifically because
// house's step-ca provisioner issues every bridge and housed itself a cert
// from one shared CA - "the trust boundary here is this internal network,
// not per-host isolation" (house-config's own docs) - so chain validation
// alone would let any house service holding a valid cert connect, not just
// the house aggregator this bridge is meant to serve. The house repo's own
// convention names the client's cert "<name>.<host>.house.internal";
// bridge.client-name is that same name, the mirror image of House's
// client-side server-name override (ui/controllers/house.go).
func verifyClientName(name string) func(_ [][]byte, verifiedChains [][]*x509.Certificate) error {
	return func(_ [][]byte, verifiedChains [][]*x509.Certificate) error {
		for _, chain := range verifiedChains {
			if len(chain) == 0 {
				continue
			}
			for _, dnsName := range chain[0].DNSNames {
				if dnsName == name {
					return nil
				}
			}
		}
		return fmt.Errorf("client certificate is not authorized for this bridge (want DNS name %q)", name)
	}
}
