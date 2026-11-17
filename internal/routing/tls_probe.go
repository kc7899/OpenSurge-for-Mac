package routing

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"
)

// Public hostnames only; no login, HTTP request, CA installation or pinning
// assumptions. A successful TLS handshake says nothing about App sign-in or
// iPhone-specific trust policies. Standard hostname/chain verification is kept.
var chatGPTTLSHosts = []string{"chatgpt.com", "api.openai.com"}

func probeServiceTLS(ctx context.Context, proxy string, hosts []string, roots *x509.CertPool) ProbeResult {
	for _, host := range hosts {
		err := handshakeViaProxy(ctx, proxy, host, roots)
		if err != nil {
			return ProbeResult{ServiceHost: host, CertificateError: certificateErrorCode(err), transportError: err}
		}
	}
	return ProbeResult{Reachable: true}
}

func handshakeViaProxy(ctx context.Context, proxy, host string, roots *x509.CertPool) error {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", proxy)
	if err != nil {
		return err
	}
	defer conn.Close()
	deadline, _ := ctx.Deadline()
	_ = conn.SetDeadline(deadline)
	address := net.JoinHostPort(host, "443")
	if _, err = fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", address, address); err != nil {
		return err
	}
	resp, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: http.MethodConnect})
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("probe tunnel unavailable")
	}
	secure := tls.Client(conn, &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12, RootCAs: roots})
	return secure.HandshakeContext(ctx)
}

func certificateErrorCode(err error) string {
	var hostname x509.HostnameError
	var authority x509.UnknownAuthorityError
	var invalid x509.CertificateInvalidError
	if errors.As(err, &hostname) {
		return "hostname_mismatch"
	}
	if errors.As(err, &authority) {
		return "untrusted_chain"
	}
	if errors.As(err, &invalid) {
		if invalid.Reason == x509.Expired {
			return "invalid_validity"
		}
		return "invalid_certificate"
	}
	return ""
}
