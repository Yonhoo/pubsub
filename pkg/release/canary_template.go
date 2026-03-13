package release

import (
	"bytes"
	"fmt"

	"gopkg.in/yaml.v3"
)

// PhaseCanaryPlan defines the release template structure for one phase rollout.
type PhaseCanaryPlan struct {
	Phase        string        `yaml:"phase"`
	Owner        string        `yaml:"owner"`
	Service      string        `yaml:"service"`
	Version      string        `yaml:"version"`
	TrafficSteps []TrafficStep `yaml:"traffic_steps"`
	Rollback     RollbackPlan  `yaml:"rollback"`
}

type TrafficStep struct {
	Name            string   `yaml:"name"`
	TrafficPercent  int      `yaml:"traffic_percent"`
	ObserveSeconds  int      `yaml:"observe_seconds"`
	SuccessCriteria []string `yaml:"success_criteria"`
	AbortCriteria   []string `yaml:"abort_criteria"`
}

type RollbackPlan struct {
	Trigger string `yaml:"trigger"`
	Switch  string `yaml:"switch"`
	Action  string `yaml:"action"`
	Verify  string `yaml:"verify"`
}

func ParsePhaseCanaryPlan(content []byte) (PhaseCanaryPlan, error) {
	var plan PhaseCanaryPlan
	decoder := yaml.NewDecoder(bytes.NewReader(content))
	decoder.KnownFields(true)
	if err := decoder.Decode(&plan); err != nil {
		return PhaseCanaryPlan{}, fmt.Errorf("decode canary template: %w", err)
	}
	if err := plan.Validate(); err != nil {
		return PhaseCanaryPlan{}, err
	}
	return plan, nil
}

func (p PhaseCanaryPlan) Validate() error {
	if p.Phase == "" {
		return fmt.Errorf("phase is required")
	}
	if p.Owner == "" {
		return fmt.Errorf("owner is required")
	}
	if p.Service == "" {
		return fmt.Errorf("service is required")
	}
	if p.Version == "" {
		return fmt.Errorf("version is required")
	}
	if len(p.TrafficSteps) == 0 {
		return fmt.Errorf("traffic_steps must contain at least one step")
	}

	lastPercent := 0
	for i, step := range p.TrafficSteps {
		stepIndex := i + 1
		if step.Name == "" {
			return fmt.Errorf("traffic_steps[%d].name is required", stepIndex)
		}
		if step.TrafficPercent < 1 || step.TrafficPercent > 100 {
			return fmt.Errorf("traffic_steps[%d].traffic_percent must be between 1 and 100", stepIndex)
		}
		if step.TrafficPercent < lastPercent {
			return fmt.Errorf("traffic_steps[%d].traffic_percent must be monotonic non-decreasing", stepIndex)
		}
		if step.ObserveSeconds <= 0 {
			return fmt.Errorf("traffic_steps[%d].observe_seconds must be > 0", stepIndex)
		}
		if len(step.SuccessCriteria) == 0 {
			return fmt.Errorf("traffic_steps[%d].success_criteria must contain at least one rule", stepIndex)
		}
		if len(step.AbortCriteria) == 0 {
			return fmt.Errorf("traffic_steps[%d].abort_criteria must contain at least one rule", stepIndex)
		}
		lastPercent = step.TrafficPercent
	}
	if lastPercent != 100 {
		return fmt.Errorf("last traffic step must be 100 percent")
	}

	if p.Rollback.Trigger == "" {
		return fmt.Errorf("rollback.trigger is required")
	}
	if p.Rollback.Switch == "" {
		return fmt.Errorf("rollback.switch is required")
	}
	if p.Rollback.Action == "" {
		return fmt.Errorf("rollback.action is required")
	}
	if p.Rollback.Verify == "" {
		return fmt.Errorf("rollback.verify is required")
	}
	return nil
}
