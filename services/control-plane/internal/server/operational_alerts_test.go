package server

import (
	"bytes"
	"log"
	"strings"
	"testing"
)

func TestControlledPilotOperationalAlertsLogOnlyRedactedTransitions(t *testing.T) {
	app := newControlPlaneApp()
	var logs bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(previous) })

	active := map[string]any{
		"accountId": "acct-secret", "operationId": "workspace-launch-secret",
		"alerts": []any{
			map[string]any{"code": "controlled_pilot_config_invalid"},
			map[string]any{"code": "controlled_pilot_first_failure"},
			map[string]any{"code": "controlled_pilot_capacity_exhausted"},
		},
	}
	app.observeControlledPilotAlerts(active)
	app.observeControlledPilotAlerts(active)
	app.observeControlledPilotAlerts(map[string]any{"alerts": []any{}})
	output := logs.String()
	for _, code := range []string{"controlled_pilot_config_invalid", "controlled_pilot_first_failure", "controlled_pilot_capacity_exhausted"} {
		if strings.Count(output, "code="+code+" state=active") != 1 || strings.Count(output, "code="+code+" state=recovered") != 1 {
			t.Fatalf("alert transition for %s was not deduplicated: %s", code, output)
		}
	}
	if strings.Contains(output, "acct-secret") || strings.Contains(output, "workspace-launch-secret") {
		t.Fatalf("controlled Pilot alert leaked identifiers: %s", output)
	}
}
