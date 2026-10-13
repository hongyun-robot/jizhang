package config

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestFundEnvironmentAndSecretEncoding(t *testing.T) {
	t.Setenv("FUYAO_API_KEY", "test-private-credential")
	c := Load()
	if c.FuyaoAPIKey != "test-private-credential" {
		t.Fatal("environment not applied")
	}
	b, e := json.Marshal(c)
	if e != nil || strings.Contains(string(b), "test-private-credential") {
		t.Fatal("credential exposed")
	}
	t.Setenv("FUYAO_API_KEY", "")
	c = Load()
	if c.FuyaoAPIKey != "" {
		t.Fatal("missing key must retain normal defaults")
	}
}
