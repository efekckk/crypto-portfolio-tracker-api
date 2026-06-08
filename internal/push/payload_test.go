package push

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/efekckk/crypto-portfolio-tracker-api/internal/domain"
	"github.com/efekckk/crypto-portfolio-tracker-api/internal/eval"
	"github.com/efekckk/crypto-portfolio-tracker-api/internal/storage"
)

func mkFiring(condition domain.AlertCondition, recurrence domain.Recurrence, actual *float64, coinName *string) eval.Firing {
	return eval.Firing{
		DeviceID: uuid.New(),
		Alert: domain.PriceAlert{
			ID:         uuid.NewString(),
			Condition:  condition,
			Recurrence: recurrence,
			IsActive:   true,
		},
		FiredAt:     time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC),
		ActualValue: actual,
		CoinName:    coinName,
	}
}

func mkDevice(locale string) storage.Device {
	return storage.Device{
		DeviceID: uuid.New(), APNsToken: "tok", APNsEnv: "development", Locale: locale,
	}
}

func strPtr(s string) *string  { return &s }
func fPtr(v float64) *float64 { return &v }

func TestPayload_PriceCrossing_EnglishWithActual(t *testing.T) {
	f := mkFiring(
		domain.PriceCrossing{CoinID: "bitcoin", Direction: domain.Above, TargetPrice: 75000},
		domain.OneShot{}, fPtr(80123), strPtr("Bitcoin"),
	)
	body := bodyForFiring(f, "en")
	if !strings.Contains(body, "Bitcoin") || !strings.Contains(body, "80123") {
		t.Fatalf("unexpected body: %s", body)
	}
}

func TestPayload_PriceCrossing_TurkishWithActual(t *testing.T) {
	f := mkFiring(
		domain.PriceCrossing{CoinID: "bitcoin", Direction: domain.Above, TargetPrice: 75000},
		domain.OneShot{}, fPtr(80123), strPtr("Bitcoin"),
	)
	body := bodyForFiring(f, "tr")
	if !strings.Contains(body, "değerine ulaştı") || !strings.Contains(body, "Bitcoin") {
		t.Fatalf("unexpected tr body: %s", body)
	}
}

func TestPayload_PercentChange_WithActual_ReportsMeasured(t *testing.T) {
	f := mkFiring(
		domain.PercentChange{CoinID: "eth", Direction: domain.Above, Window: domain.Window24h, Threshold: 5},
		domain.OneShot{}, fPtr(8.2), strPtr("Ethereum"),
	)
	body := bodyForFiring(f, "en")
	if !strings.Contains(body, "Ethereum") {
		t.Fatalf("missing coin name: %s", body)
	}
	if !strings.Contains(body, "8.2") {
		t.Fatalf("missing actual percent: %s", body)
	}
	if !strings.Contains(body, "24h") {
		t.Fatalf("missing window: %s", body)
	}
	if strings.Contains(body, "crossed") {
		t.Fatalf("with actual should say moved, got: %s", body)
	}
}

func TestPayload_PercentChange_FallbackToThreshold(t *testing.T) {
	f := mkFiring(
		domain.PercentChange{CoinID: "eth", Direction: domain.Above, Window: domain.Window24h, Threshold: 5},
		domain.OneShot{}, nil, nil,
	)
	body := bodyForFiring(f, "en")
	if !strings.Contains(body, "crossed") {
		t.Fatalf("without actual should say crossed, got: %s", body)
	}
	if !strings.Contains(body, "5") {
		t.Fatalf("missing threshold: %s", body)
	}
}

func TestPayload_PortfolioValue_EN(t *testing.T) {
	f := mkFiring(
		domain.PortfolioValue{Direction: domain.Above, Threshold: 100000},
		domain.OneShot{}, fPtr(120000), nil,
	)
	body := bodyForFiring(f, "en")
	if !strings.Contains(body, "Portfolio total reached") || !strings.Contains(body, "120000") {
		t.Fatalf("unexpected: %s", body)
	}
}

func TestPayload_PortfolioPnLPercent_TR(t *testing.T) {
	f := mkFiring(
		domain.PortfolioPnLPercent{Direction: domain.Below, Threshold: -10},
		domain.OneShot{}, fPtr(-22), nil,
	)
	body := bodyForFiring(f, "tr")
	if !strings.Contains(body, "K/Z şimdi") || !strings.Contains(body, "-22") {
		t.Fatalf("unexpected: %s", body)
	}
}

func TestPayload_CoinName_FallsBackToCapitalizedID(t *testing.T) {
	f := mkFiring(
		domain.PriceCrossing{CoinID: "bitcoin", Direction: domain.Above, TargetPrice: 1},
		domain.OneShot{}, fPtr(2), nil, // no coin name resolved
	)
	body := bodyForFiring(f, "en")
	if !strings.Contains(body, "Bitcoin") {
		t.Fatalf("expected capitalised coin id, got: %s", body)
	}
}

func TestPayload_FullShape_IncludesAPSAndCustomFields(t *testing.T) {
	f := mkFiring(
		domain.PriceCrossing{CoinID: "bitcoin", Direction: domain.Above, TargetPrice: 75000},
		domain.OneShot{}, fPtr(80000), strPtr("Bitcoin"),
	)
	out := Payload(f, mkDevice("en"))
	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	aps, ok := decoded["aps"].(map[string]any)
	if !ok {
		t.Fatalf("missing aps: %v", decoded)
	}
	alert, ok := aps["alert"].(map[string]any)
	if !ok {
		t.Fatalf("missing alert: %v", aps)
	}
	if alert["title"] != "Alert" {
		t.Fatalf("title: %v", alert["title"])
	}
	if alert["body"] == "" {
		t.Fatalf("empty body")
	}
	if aps["sound"] != "default" {
		t.Fatalf("sound: %v", aps["sound"])
	}
	if aps["thread-id"] != f.Alert.ID {
		t.Fatalf("thread-id: %v", aps["thread-id"])
	}
	if decoded["alert_id"] != f.Alert.ID {
		t.Fatalf("alert_id: %v", decoded["alert_id"])
	}
	if decoded["coin_name"] != "Bitcoin" {
		t.Fatalf("coin_name: %v", decoded["coin_name"])
	}
	if decoded["actual_value"].(float64) != 80000 {
		t.Fatalf("actual_value: %v", decoded["actual_value"])
	}
}

func TestPayload_OnCrossing_IncludesNewLastConditionResult(t *testing.T) {
	lcr := true
	f := mkFiring(
		domain.PriceCrossing{CoinID: "bitcoin", Direction: domain.Above, TargetPrice: 1},
		domain.OnCrossing{}, fPtr(2), strPtr("Bitcoin"),
	)
	f.Alert.LastConditionResult = &lcr
	out := Payload(f, mkDevice("en"))
	if v, ok := out["new_last_condition_result"]; !ok || v != true {
		t.Fatalf("expected new_last_condition_result=true, got %v", out)
	}
}

func TestPayload_OneShot_OmitsNewLastConditionResult(t *testing.T) {
	f := mkFiring(
		domain.PriceCrossing{CoinID: "bitcoin", Direction: domain.Above, TargetPrice: 1},
		domain.OneShot{}, fPtr(2), strPtr("Bitcoin"),
	)
	out := Payload(f, mkDevice("en"))
	if _, ok := out["new_last_condition_result"]; ok {
		t.Fatalf("oneShot should not include new_last_condition_result")
	}
}
