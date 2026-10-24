package actionrpc

import (
	"bytes"
	"strings"
	"testing"

	"github.com/hashicorp/go-hclog"
)

// A credential longer than the log truncates values to must not leave its
// first part in the log: the runner hides only values it finds whole.
func TestLogHidesCredentialsBeforeTruncating(t *testing.T) {
	token := "eyJhbGciOiJIUzI1NiJ9." + strings.Repeat("A", 300)
	password := strings.Repeat("p", 250)
	dsnPassword := strings.Repeat("d", 250)

	with := map[string]any{
		"url":      "http://example.com",
		"headers":  map[string]any{"Authorization": "Bearer " + token},
		"password": password,
		"dsn":      "postgres://app:" + dsnPassword + "@db/main",
	}

	var buf bytes.Buffer
	log := hclog.New(&hclog.LoggerOptions{Output: &buf, Level: hclog.Trace})
	LogParams(log, "params", with)
	LogOutcome(log, "request", map[string]any{"req": with, "status": 0}, nil)

	out := buf.String()
	for name, secret := range map[string]string{"token": token, "password": password, "dsn password": dsnPassword} {
		if strings.Contains(out, secret[:40]) {
			t.Errorf("the log holds part of the %s:\n%s", name, out)
		}
	}
	if !strings.Contains(out, "http://example.com") {
		t.Errorf("the log lost the parameters that are not credentials:\n%s", out)
	}
	if with["password"] != password {
		t.Error("logging changed the action's parameters")
	}
}
