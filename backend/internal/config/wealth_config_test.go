package config

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestWealthCollectorConfigurationDoesNotExposeKey(t *testing.T) {
	const secret = "test-private-collector-key-123456"
	t.Setenv("WEALTH_COLLECTOR_URL", "http://127.0.0.1:8091")
	t.Setenv("WEALTH_COLLECTOR_KEY", secret)
	c := Load()
	if c.WealthCollectorURL != "http://127.0.0.1:8091" || c.WealthCollectorKey != secret {
		t.Fatal("collector configuration not applied")
	}
	body, err := json.Marshal(c)
	if err != nil || strings.Contains(string(body), secret) {
		t.Fatal("collector key leaked in configuration JSON")
	}
	t.Setenv("WEALTH_COLLECTOR_KEY", "")
	t.Setenv("WEALTH_COLLECTOR_URL", "")
	if c = Load(); c.WealthCollectorURL != "" || c.WealthCollectorKey != "" {
		t.Fatal("collector must be opt-in")
	}
}
