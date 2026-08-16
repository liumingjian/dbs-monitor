package httpapi

import "testing"

func TestAgentVersionSupported(t *testing.T) {
	tests := []struct {
		name   string
		agent  string
		server string
		want   bool
	}{
		{name: "current major", agent: "3.1.4", server: "3.0.0", want: true},
		{name: "previous major", agent: "2.9.0", server: "3.0.0", want: true},
		{name: "too old", agent: "1.99.0", server: "3.0.0", want: false},
		{name: "future version", agent: "4.0.0", server: "3.0.0", want: true},
		{name: "invalid agent version", agent: "dev", server: "3.0.0", want: false},
		{name: "invalid server version", agent: "3.0.0", server: "dev", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := agentVersionSupported(tt.agent, tt.server); got != tt.want {
				t.Fatalf("agentVersionSupported(%q, %q) = %v, want %v", tt.agent, tt.server, got, tt.want)
			}
		})
	}
}
