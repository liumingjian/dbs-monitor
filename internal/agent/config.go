package agent

import (
	"fmt"
	"net/url"

	"github.com/google/uuid"
)

type Config struct {
	ServerURL string
	Instance  uuid.UUID
	Token     string
	CAFile    string
}

func ParseConfig(serverURL, instanceID, token, caFile string) (Config, error) {
	instance, err := uuid.Parse(instanceID)
	if err != nil {
		return Config{}, fmt.Errorf("INSTANCE_ID must be a UUID")
	}
	if serverURL == "" || token == "" || caFile == "" {
		return Config{}, fmt.Errorf("SERVER_URL, INSTANCE_ID, AGENT_TOKEN, and CA_FILE are required")
	}
	parsed, err := url.Parse(serverURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return Config{}, fmt.Errorf("SERVER_URL must be an HTTPS URL without user information")
	}
	return Config{ServerURL: serverURL, Instance: instance, Token: token, CAFile: caFile}, nil
}
