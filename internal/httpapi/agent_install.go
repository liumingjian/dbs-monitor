package httpapi

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
)

//go:embed agent-install.sh
var agentInstaller []byte

type AgentDistribution struct {
	CAPath          string
	BinaryDirectory string
	CAFingerprint   string
}

func LoadAgentDistribution(caPath, binaryDirectory string) (AgentDistribution, error) {
	caPEM, err := os.ReadFile(caPath)
	if err != nil {
		return AgentDistribution{}, fmt.Errorf("read Agent distribution CA: %w", err)
	}
	certificate, _ := pem.Decode(caPEM)
	if certificate == nil || certificate.Type != "CERTIFICATE" {
		return AgentDistribution{}, fmt.Errorf("Agent distribution CA is not a PEM certificate")
	}
	fingerprint := sha256.Sum256(certificate.Bytes)
	return AgentDistribution{
		CAPath:          caPath,
		BinaryDirectory: binaryDirectory,
		CAFingerprint: hex.EncodeToString(fingerprint[:]),
	}, nil
}

func (handler *Handler) registerAgentDistributionRoutes(mux *http.ServeMux) {
	if handler.agentDistribution == nil {
		return
	}
	mux.HandleFunc("GET /api/agent/install/install.sh", func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
		writer.Header().Set("Cache-Control", "no-store")
		_, _ = writer.Write(agentInstaller)
	})
	mux.HandleFunc("GET /api/agent/install/ca.crt", func(writer http.ResponseWriter, request *http.Request) {
		http.ServeFile(writer, request, handler.agentDistribution.CAPath)
	})
	mux.HandleFunc("GET /api/agent/install/dbs-monitor-agent/{arch}", func(writer http.ResponseWriter, request *http.Request) {
		arch := request.PathValue("arch")
		if arch != "amd64" && arch != "arm64" {
			http.NotFound(writer, request)
			return
		}
		path := filepath.Join(handler.agentDistribution.BinaryDirectory, "dbs-monitor-agent-linux-"+arch)
		if _, err := os.Stat(path); err != nil {
			http.NotFound(writer, request)
			return
		}
		writer.Header().Set("Content-Type", "application/octet-stream")
		writer.Header().Set("Content-Disposition", "attachment; filename=dbs-monitor-agent")
		http.ServeFile(writer, request, path)
	})
}
