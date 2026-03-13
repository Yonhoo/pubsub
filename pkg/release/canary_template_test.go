package release

import (
	"strings"
	"testing"
)

func TestParsePhaseCanaryPlan_OK(t *testing.T) {
	content := []byte(`
phase: "phase-1"
owner: "oncall"
service: "connect-node"
version: "v1"
traffic_steps:
  - name: "10%"
    traffic_percent: 10
    observe_seconds: 300
    success_criteria:
      - "ok"
    abort_criteria:
      - "bad"
  - name: "100%"
    traffic_percent: 100
    observe_seconds: 600
    success_criteria:
      - "ok"
    abort_criteria:
      - "bad"
rollback:
  trigger: "abort"
  switch: "switch back"
  action: "rollback"
  verify: "recovered"
`)

	_, err := ParsePhaseCanaryPlan(content)
	if err != nil {
		t.Fatalf("ParsePhaseCanaryPlan returned error: %v", err)
	}
}

func TestParsePhaseCanaryPlan_InvalidLastStep(t *testing.T) {
	content := []byte(`
phase: "phase-1"
owner: "oncall"
service: "connect-node"
version: "v1"
traffic_steps:
  - name: "10%"
    traffic_percent: 10
    observe_seconds: 300
    success_criteria:
      - "ok"
    abort_criteria:
      - "bad"
rollback:
  trigger: "abort"
  switch: "switch back"
  action: "rollback"
  verify: "recovered"
`)

	_, err := ParsePhaseCanaryPlan(content)
	if err == nil {
		t.Fatalf("expected error but got nil")
	}
	if !strings.Contains(err.Error(), "last traffic step must be 100 percent") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParsePhaseCanaryPlan_MissingRollbackSwitch(t *testing.T) {
	content := []byte(`
phase: "phase-1"
owner: "oncall"
service: "connect-node"
version: "v1"
traffic_steps:
  - name: "100%"
    traffic_percent: 100
    observe_seconds: 300
    success_criteria:
      - "ok"
    abort_criteria:
      - "bad"
rollback:
  trigger: "abort"
  action: "rollback"
  verify: "recovered"
`)

	_, err := ParsePhaseCanaryPlan(content)
	if err == nil {
		t.Fatalf("expected error but got nil")
	}
	if !strings.Contains(err.Error(), "rollback.switch is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParsePhaseCanaryPlan_MonotonicTrafficPercent(t *testing.T) {
	content := []byte(`
phase: "phase-1"
owner: "oncall"
service: "connect-node"
version: "v1"
traffic_steps:
  - name: "30%"
    traffic_percent: 30
    observe_seconds: 300
    success_criteria:
      - "ok"
    abort_criteria:
      - "bad"
  - name: "20%"
    traffic_percent: 20
    observe_seconds: 300
    success_criteria:
      - "ok"
    abort_criteria:
      - "bad"
  - name: "100%"
    traffic_percent: 100
    observe_seconds: 300
    success_criteria:
      - "ok"
    abort_criteria:
      - "bad"
rollback:
  trigger: "abort"
  switch: "switch back"
  action: "rollback"
  verify: "recovered"
`)

	_, err := ParsePhaseCanaryPlan(content)
	if err == nil {
		t.Fatalf("expected error but got nil")
	}
	if !strings.Contains(err.Error(), "monotonic") {
		t.Fatalf("unexpected error: %v", err)
	}
}
