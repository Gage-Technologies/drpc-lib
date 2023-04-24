package muxconn

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
)

// DialTlsProxy
//
//	Dials a TLS connection via an HTTP proxy
func DialTlsProxy(target string, proxyURL *url.URL, tlsConfig *tls.Config) (net.Conn, error) {
	// connect to the proxy server
	proxyConn, err := net.Dial("tcp", proxyURL.Host)
	if err != nil {
		return nil, fmt.Errorf("failed to dial proxy: %w", err)
	}

	// send a CONNECT request to the proxy server
	req := &http.Request{
		Method: "CONNECT",
		URL:    &url.URL{Opaque: target},
		Header: make(http.Header),
	}
	err = req.Write(proxyConn)
	if err != nil {
		_ = proxyConn.Close()
		return nil, fmt.Errorf("failed to send CONNECT request to proxy: %w", err)
	}

	// read and check the response from the proxy server
	resp, err := http.ReadResponse(bufio.NewReader(proxyConn), req)
	if err != nil {
		_ = proxyConn.Close()
		return nil, fmt.Errorf("failed to read response from proxy: %w", err)
	}
	if resp.StatusCode != 200 {
		_ = proxyConn.Close()
		return nil, fmt.Errorf("failed to connect to proxy server, status code: %d", resp.StatusCode)
	}

	// create a TLS connection over the established proxy connection
	tlsConfig.ServerName = target
	tlsConn := tls.Client(proxyConn, tlsConfig)

	// perform the TLS handshake
	err = tlsConn.Handshake()
	if err != nil {
		_ = proxyConn.Close()
		return nil, fmt.Errorf("failed to perform TLS handshake: %w", err)
	}

	return tlsConn, nil
}
