package agent

import "testing"

func TestParseConfigRequiresHTTPS(t *testing.T) {
	instance := "00000000-0000-0000-0000-000000000001"
	for _, address := range []string{"http://monitor.example", "monitor.example", "https://user@monitor.example"} {
		t.Run(address, func(t *testing.T) {
			if _, err := ParseConfig(address, instance, "token", "/tmp/ca.crt"); err == nil {
				t.Fatalf("ParseConfig accepted %q", address)
			}
		})
	}
	if _, err := ParseConfig("https://monitor.example", instance, "token", "/tmp/ca.crt"); err != nil {
		t.Fatalf("ParseConfig rejected HTTPS URL: %v", err)
	}
}
