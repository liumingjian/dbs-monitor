package main

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestSecurityHeadersMatchB13Golden(t *testing.T) {
	want, err := os.ReadFile("testdata/security_headers.golden")
	if err != nil {
		t.Fatalf("read B13 security header golden: %v", err)
	}

	var got strings.Builder
	for _, header := range securityResponseHeaders {
		fmt.Fprintf(&got, "%s: %s\n", header.Name, header.Value)
	}
	if got.String() != string(want) {
		t.Fatalf("security headers changed:\n--- got ---\n%s--- want ---\n%s", got.String(), want)
	}
}

func TestSecurityHeadersWrapAPIAndStaticHandlers(t *testing.T) {
	handler := securityHeadersHandler(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	}))

	for _, path := range []string{"/api/v1/diagnostics/health", "/"} {
		t.Run(path, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))

			if response.Code != http.StatusNoContent {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
			}
			for _, header := range securityResponseHeaders {
				if got := response.Header().Get(header.Name); got != header.Value {
					t.Errorf("%s = %q, want %q", header.Name, got, header.Value)
				}
			}
		})
	}
}

func TestServerTLSConfigRejectsTLS12AndAcceptsTLS13(t *testing.T) {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	}))
	server.TLS = serverTLSConfig()
	server.StartTLS()
	defer server.Close()

	tls12Client := server.Client()
	tls12Transport := tls12Client.Transport.(*http.Transport).Clone()
	tls12Transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true, MaxVersion: tls.VersionTLS12}
	tls12Client.Transport = tls12Transport
	defer tls12Transport.CloseIdleConnections()
	if response, err := tls12Client.Get(server.URL); err == nil {
		response.Body.Close()
		t.Fatal("TLS 1.2 handshake succeeded, want rejection")
	}

	tls13Client := server.Client()
	tls13Transport := tls13Client.Transport.(*http.Transport).Clone()
	tls13Transport.TLSClientConfig = &tls.Config{
		InsecureSkipVerify: true,
		MinVersion:         tls.VersionTLS13,
		MaxVersion:         tls.VersionTLS13,
	}
	tls13Client.Transport = tls13Transport
	defer tls13Transport.CloseIdleConnections()
	response, err := tls13Client.Get(server.URL)
	if err != nil {
		t.Fatalf("TLS 1.3 request: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("TLS 1.3 status = %d, want %d", response.StatusCode, http.StatusNoContent)
	}
}
