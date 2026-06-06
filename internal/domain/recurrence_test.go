package domain

import (
	"strings"
	"testing"
)

func TestRecurrence_RoundTrip_OneShot(t *testing.T) {
	data, err := MarshalRecurrence(OneShot{})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(data) != `{"type":"oneShot"}` {
		t.Fatalf("unexpected wire: %s", data)
	}
	decoded, err := UnmarshalRecurrence(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := decoded.(OneShot); !ok {
		t.Fatalf("expected OneShot, got %T", decoded)
	}
}

func TestRecurrence_RoundTrip_Cooldown_PreservesInterval(t *testing.T) {
	data, err := MarshalRecurrence(Cooldown{Seconds: 3600})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	decoded, err := UnmarshalRecurrence(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	got, ok := decoded.(Cooldown)
	if !ok {
		t.Fatalf("expected Cooldown, got %T", decoded)
	}
	if got.Seconds != 3600 {
		t.Fatalf("expected 3600, got %d", got.Seconds)
	}
}

func TestRecurrence_RoundTrip_OnCrossing(t *testing.T) {
	data, err := MarshalRecurrence(OnCrossing{})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	decoded, err := UnmarshalRecurrence(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := decoded.(OnCrossing); !ok {
		t.Fatalf("expected OnCrossing, got %T", decoded)
	}
}

func TestRecurrence_UnmarshalRejectsUnknownType(t *testing.T) {
	_, err := UnmarshalRecurrence([]byte(`{"type":"madeUp"}`))
	if err == nil {
		t.Fatal("expected error for unknown type, got nil")
	}
	if !strings.Contains(err.Error(), "unknown recurrence type") {
		t.Fatalf("expected error mentioning unknown type, got: %v", err)
	}
}

func TestRecurrence_UnmarshalRejectsMissingType(t *testing.T) {
	_, err := UnmarshalRecurrence([]byte(`{"seconds":3600}`))
	if err == nil {
		t.Fatal("expected error for missing type, got nil")
	}
}
