package domain

import (
	"encoding/json"
	"fmt"
)

// Recurrence is how often a satisfied condition is allowed to fire.
// Use `MarshalRecurrence` / `UnmarshalRecurrence` for JSON.
type Recurrence interface {
	isRecurrence()
}

// OneShot fires once, then deactivates itself.
type OneShot struct{}

func (OneShot) isRecurrence() {}

// Cooldown fires whenever the condition is true AND `Seconds` have elapsed
// since the previous firing.
type Cooldown struct {
	Seconds int64 `json:"seconds"`
}

func (Cooldown) isRecurrence() {}

// OnCrossing fires on each false→true transition of the condition.
type OnCrossing struct{}

func (OnCrossing) isRecurrence() {}

// MarshalRecurrence serialises a recurrence with a `"type"` discriminator.
func MarshalRecurrence(r Recurrence) ([]byte, error) {
	switch v := r.(type) {
	case OneShot:
		return json.Marshal(map[string]any{"type": "oneShot"})
	case Cooldown:
		return json.Marshal(map[string]any{"type": "cooldown", "seconds": v.Seconds})
	case OnCrossing:
		return json.Marshal(map[string]any{"type": "onCrossing"})
	default:
		return nil, fmt.Errorf("domain: cannot marshal recurrence of type %T", r)
	}
}

// UnmarshalRecurrence parses a recurrence by reading the `"type"` field.
func UnmarshalRecurrence(data []byte) (Recurrence, error) {
	var env struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("domain: decode recurrence envelope: %w", err)
	}
	switch env.Type {
	case "oneShot":
		return OneShot{}, nil
	case "cooldown":
		var v Cooldown
		if err := json.Unmarshal(data, &v); err != nil {
			return nil, err
		}
		return v, nil
	case "onCrossing":
		return OnCrossing{}, nil
	case "":
		return nil, fmt.Errorf("domain: recurrence missing required %q field", "type")
	default:
		return nil, fmt.Errorf("domain: unknown recurrence type %q", env.Type)
	}
}
