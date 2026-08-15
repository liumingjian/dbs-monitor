package main

import (
	"crypto/tls"
	"net/http"
)

type securityResponseHeader struct {
	Name  string
	Value string
}

var securityResponseHeaders = [...]securityResponseHeader{
	{
		Name: "Content-Security-Policy",
		// AntD injects runtime style elements; allowing inline styles avoids rewriting embedded HTML while scripts remain strict.
		Value: "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'self'; frame-ancestors 'none'",
	},
	{Name: "Strict-Transport-Security", Value: "max-age=31536000"},
	{Name: "X-Content-Type-Options", Value: "nosniff"},
	{Name: "X-Frame-Options", Value: "DENY"},
	{Name: "Referrer-Policy", Value: "no-referrer"},
	{Name: "Permissions-Policy", Value: "camera=(), microphone=(), geolocation=(), interest-cohort=()"},
}

func securityHeadersHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		for _, header := range securityResponseHeaders {
			writer.Header().Set(header.Name, header.Value)
		}
		next.ServeHTTP(writer, request)
	})
}

func serverTLSConfig() *tls.Config {
	return &tls.Config{MinVersion: tls.VersionTLS13}
}
