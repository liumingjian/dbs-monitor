package main

import (
	"crypto/tls"
	"net/http"
)

type securityResponseHeader struct {
	name  string
	value string
}

var securityResponseHeaders = [...]securityResponseHeader{
	{
		name: "Content-Security-Policy",
		// AntD injects runtime style elements; allowing inline styles avoids rewriting embedded HTML while scripts remain strict.
		value: "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'self'; frame-ancestors 'none'",
	},
	{name: "Strict-Transport-Security", value: "max-age=31536000"},
	{name: "X-Content-Type-Options", value: "nosniff"},
	{name: "X-Frame-Options", value: "DENY"},
	{name: "Referrer-Policy", value: "no-referrer"},
	{name: "Permissions-Policy", value: "camera=(), microphone=(), geolocation=(), interest-cohort=()"},
}

func securityHeadersHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		for _, header := range securityResponseHeaders {
			writer.Header().Set(header.name, header.value)
		}
		next.ServeHTTP(writer, request)
	})
}

func serverTLSConfig() *tls.Config {
	return &tls.Config{MinVersion: tls.VersionTLS13}
}
