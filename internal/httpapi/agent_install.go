package httpapi

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/liumingjian/dbs-monitor/internal/api"
)

//go:embed agent-install.sh
var agentInstaller []byte

type AgentDistribution struct {
	CAPath          string
	BinaryDirectory string
	CAFingerprint   string
}

func (distribution AgentDistribution) HealthError() error {
	if distribution.BinaryDirectory == "" {
		return fmt.Errorf("AGENT_BINARY_DIR is not configured")
	}
	directory, err := os.Stat(distribution.BinaryDirectory)
	if err != nil {
		return fmt.Errorf("AGENT_BINARY_DIR is unavailable: %w", err)
	}
	if !directory.IsDir() {
		return fmt.Errorf("AGENT_BINARY_DIR is not a directory")
	}
	for _, architecture := range []string{"amd64", "arm64"} {
		path := filepath.Join(distribution.BinaryDirectory, "dbs-monitor-agent-linux-"+architecture)
		binary, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("AGENT_BINARY_DIR is missing the linux/%s binary: %w", architecture, err)
		}
		if !binary.Mode().IsRegular() || binary.Mode().Perm()&0111 == 0 {
			return fmt.Errorf("AGENT_BINARY_DIR linux/%s binary is not an executable regular file", architecture)
		}
	}
	return nil
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
		CAFingerprint:   hex.EncodeToString(fingerprint[:]),
	}, nil
}

func (handler *Handler) DownloadAgentBinary(_ context.Context, request api.DownloadAgentBinaryRequestObject) (api.DownloadAgentBinaryResponseObject, error) {
	var architecture string
	switch request.Params.Arch {
	case api.Linuxamd64:
		architecture = "amd64"
	case api.Linuxarm64:
		architecture = "arm64"
	default:
		return api.DownloadAgentBinary400JSONResponse(errorBody(api.VALIDATIONFAILED, "unsupported Agent architecture")), nil
	}
	if handler.agentDistribution == nil || handler.agentDistribution.HealthError() != nil {
		return api.DownloadAgentBinary503JSONResponse(errorBody(api.INTERNAL, "AGENT_BINARY_DIR is unavailable or incomplete")), nil
	}
	path := filepath.Join(handler.agentDistribution.BinaryDirectory, "dbs-monitor-agent-linux-"+architecture)
	binary, err := os.Open(path)
	if err != nil {
		return api.DownloadAgentBinary503JSONResponse(errorBody(api.INTERNAL, "Agent binary distribution is unavailable")), nil
	}
	info, err := binary.Stat()
	if err != nil {
		binary.Close()
		return nil, err
	}
	return api.DownloadAgentBinary200ApplicationoctetStreamResponse{Body: binary, ContentLength: info.Size()}, nil
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
}
