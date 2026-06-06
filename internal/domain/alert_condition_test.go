package domain

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAlertCondition_RoundTrip_PriceCrossing(t *testing.T) {
	original := PriceCrossing{CoinID: "bitcoin", Direction: Above, TargetPrice: 75000}
	data, err := MarshalAlertCondition(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// Sanity-check the wire shape.
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("re-decode wire: %v", err)
	}
	if m["type"] != "priceCrossing" {
		t.Fatalf(`expected type=priceCrossing, got %v`, m["type"])
	}
	if m["coin_id"] != "bitcoin" {
		t.Fatalf(`expected coin_id=bitcoin, got %v`, m["coin_id"])
	}

	decoded, err := UnmarshalAlertCondition(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	got, ok := decoded.(PriceCrossing)
	if !ok {
		t.Fatalf("expected PriceCrossing, got %T", decoded)
	}
	if got != original {
		t.Fatalf("round trip mismatch: %+v vs %+v", got, original)
	}
}

func TestAlertCondition_RoundTrip_PercentChange(t *testing.T) {
	original := PercentChange{CoinID: "ethereum", Direction: Below, Window: Window7d, Threshold: -5}
	data, err := MarshalAlertCondition(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	decoded, err := UnmarshalAlertCondition(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	got, ok := decoded.(PercentChange)
	if !ok {
		t.Fatalf("expected PercentChange, got %T", decoded)
	}
	if got != original {
		t.Fatalf("round trip mismatch: %+v vs %+v", got, original)
	}
}

func TestAlertCondition_RoundTrip_PortfolioValue(t *testing.T) {
	original := PortfolioValue{Direction: Above, Threshold: 100000}
	data, err := MarshalAlertCondition(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	decoded, err := UnmarshalAlertCondition(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	got, ok := decoded.(PortfolioValue)
	if !ok {
		t.Fatalf("expected PortfolioValue, got %T", decoded)
	}
	if got != original {
		t.Fatalf("round trip mismatch: %+v vs %+v", got, original)
	}
}

func TestAlertCondition_RoundTrip_PortfolioPnLPercent(t *testing.T) {
	original := PortfolioPnLPercent{Direction: Below, Threshold: -10}
	data, err := MarshalAlertCondition(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	decoded, err := UnmarshalAlertCondition(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	got, ok := decoded.(PortfolioPnLPercent)
	if !ok {
		t.Fatalf("expected PortfolioPnLPercent, got %T", decoded)
	}
	if got != original {
		t.Fatalf("round trip mismatch: %+v vs %+v", got, original)
	}
}

func TestAlertCondition_UnmarshalRejectsUnknownType(t *testing.T) {
	_, err := UnmarshalAlertCondition([]byte(`{"type":"madeUp"}`))
	if err == nil {
		t.Fatal("expected error for unknown type, got nil")
	}
	if !strings.Contains(err.Error(), "unknown condition type") {
		t.Fatalf("expected error mentioning unknown type, got: %v", err)
	}
}

func TestAlertCondition_UnmarshalRejectsMissingType(t *testing.T) {
	_, err := UnmarshalAlertCondition([]byte(`{"coin_id":"btc","target_price":1}`))
	if err == nil {
		t.Fatal("expected error for missing type, got nil")
	}
}

func TestAlertCondition_RequiredCoinIDs(t *testing.T) {
	if got := (PriceCrossing{CoinID: "btc"}).RequiredCoinIDs(); len(got) != 1 || got[0] != "btc" {
		t.Fatalf("PriceCrossing: expected [btc], got %v", got)
	}
	if got := (PercentChange{CoinID: "eth"}).RequiredCoinIDs(); len(got) != 1 || got[0] != "eth" {
		t.Fatalf("PercentChange: expected [eth], got %v", got)
	}
	if got := (PortfolioValue{}).RequiredCoinIDs(); got != nil {
		t.Fatalf("PortfolioValue: expected nil, got %v", got)
	}
	if got := (PortfolioPnLPercent{}).RequiredCoinIDs(); got != nil {
		t.Fatalf("PortfolioPnLPercent: expected nil, got %v", got)
	}
}
