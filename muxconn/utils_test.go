package muxconn

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func transfer(dst io.Writer, src io.Reader) {
	defer dst.(io.Closer).Close()
	defer src.(io.Closer).Close()
	io.Copy(dst, src)
}

func TestDialTlsProxy(t *testing.T) {
	_, b, _, _ := runtime.Caller(0)
	basepath := strings.Replace(filepath.Dir(b), "/muxconn", "", -1)

	// set up the target server with a self-signed certificate
	cert, err := tls.LoadX509KeyPair(basepath+"/_test_data/cert.pem", basepath+"/_test_data/key.pem")
	if err != nil {
		t.Fatal(err)
	}
	targetServer := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "Hello, client")
	}))
	targetServer.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	targetServer.StartTLS()
	defer targetServer.Close()

	// set up the proxy server
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodConnect {
			destConn, err := net.Dial("tcp", r.Host)
			if err != nil {
				http.Error(w, "Error connecting to target server", http.StatusBadGateway)
				return
			}
			w.WriteHeader(http.StatusOK)

			hijacker, ok := w.(http.Hijacker)
			if !ok {
				http.Error(w, "Hijacking not supported", http.StatusInternalServerError)
				return
			}
			clientConn, _, err := hijacker.Hijack()
			if err != nil {
				http.Error(w, "Error hijacking connection", http.StatusInternalServerError)
				return
			}

			go transfer(destConn, clientConn)
			go transfer(clientConn, destConn)
		} else {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	}))
	defer proxyServer.Close()

	proxyURL, _ := url.Parse(proxyServer.URL)

	// test the DialTlsProxy function
	targetURL, err := url.Parse(targetServer.URL)
	if err != nil {
		t.Fatalf("Failed to parse target server URL: %v", err)
	}
	conn, err := DialTlsProxy(targetURL.Host, proxyURL, &tls.Config{InsecureSkipVerify: true})
	if err != nil {
		t.Fatalf("Failed to dial TLS over HTTP proxy: %v", err)
	}
	defer conn.Close()

	// send a simple request to the target server
	fmt.Fprint(conn, "GET / HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n")

	reader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reader, nil)
	if err != nil {
		t.Fatalf("Error reading response from target server: %v", err)
	}

	body, _ := io.ReadAll(resp.Body)
	bodyStr := strings.TrimSpace(string(body))
	if bodyStr != "Hello, client" {
		t.Errorf("Expected response: 'Hello, client', got: '%s'", bodyStr)
	}
}
