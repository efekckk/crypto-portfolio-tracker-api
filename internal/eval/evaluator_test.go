package eval

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/efekckk/crypto-portfolio-tracker-api/internal/domain"
	"github.com/efekckk/crypto-portfolio-tracker-api/internal/storage"
)

// fakeAlertRepo and fakeHoldingRepo + fakeMarkets give us a deterministic
// in-memory evaluator harness; no docker required.

type fakeAlertRepo struct {
	active  []storage.ActiveAlert
	updates []alertUpdate
	failOn  string // method name to fail
}

type alertUpdate struct {
	ID                  uuid.UUID
	IsActive            bool
	FiredAt             *time.Time
	LastConditionResult *bool
}

func (f *fakeAlertRepo) ListActive(_ context.Context) ([]storage.ActiveAlert, error) {
	if f.failOn == "ListActive" {
		return nil, errors.New("boom")
	}
	return f.active, nil
}

func (f *fakeAlertRepo) UpdateState(_ context.Context, id uuid.UUID, isActive bool, firedAt *time.Time, lcr *bool) error {
	if f.failOn == "UpdateState" {
		return errors.New("boom")
	}
	f.updates = append(f.updates, alertUpdate{ID: id, IsActive: isActive, FiredAt: firedAt, LastConditionResult: lcr})
	return nil
}

type fakeHoldingRepo struct {
	byDevice map[uuid.UUID][]domain.Holding
}

func (f *fakeHoldingRepo) ListByDevice(_ context.Context, dev uuid.UUID) ([]domain.Holding, error) {
	return f.byDevice[dev], nil
}

type fakeMarkets struct {
	result    []domain.Coin
	callCount int
	lastIDs   []string
}

func (f *fakeMarkets) FetchMarkets(_ context.Context, ids []string, _ string) ([]domain.Coin, error) {
	f.callCount++
	f.lastIDs = ids
	return f.result, nil
}

func ptrF(v float64) *float64 { return &v }
func ptrB(v bool) *bool       { return &v }

func newEval(active []storage.ActiveAlert, holdings map[uuid.UUID][]domain.Holding, coins []domain.Coin) (*Evaluator, *fakeAlertRepo, *fakeMarkets) {
	a := &fakeAlertRepo{active: active}
	h := &fakeHoldingRepo{byDevice: holdings}
	m := &fakeMarkets{result: coins}
	return &Evaluator{Alerts: a, Holdings: h, Markets: m, Currency: "usd"}, a, m
}

func dev(label string) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte("dev:"+label))
}

func alertID(label string) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte("alert:"+label)).String()
}

func TestTick_NoActive_ReturnsEmpty(t *testing.T) {
	e, _, m := newEval(nil, nil, nil)
	firings, err := e.Tick(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("tick: %v", err)
	}
	if len(firings) != 0 {
		t.Fatalf("expected 0, got %d", len(firings))
	}
	if m.callCount != 0 {
		t.Fatalf("expected 0 markets calls, got %d", m.callCount)
	}
}

func TestTick_PriceCrossingAbove_OneShot_Fires(t *testing.T) {
	now := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)
	d := dev("a")
	id := alertID("btc-above")
	active := []storage.ActiveAlert{{DeviceID: d, Alert: domain.PriceAlert{
		ID:         id,
		Condition:  domain.PriceCrossing{CoinID: "bitcoin", Direction: domain.Above, TargetPrice: 75000},
		Recurrence: domain.OneShot{},
		IsActive:   true,
	}}}
	coins := []domain.Coin{{ID: "bitcoin", Symbol: "btc", Name: "Bitcoin", CurrentPrice: 80000}}
	e, repo, _ := newEval(active, nil, coins)

	firings, err := e.Tick(context.Background(), now)
	if err != nil {
		t.Fatalf("tick: %v", err)
	}
	if len(firings) != 1 {
		t.Fatalf("expected 1 firing, got %d", len(firings))
	}
	f := firings[0]
	if f.DeviceID != d || f.Alert.ID != id {
		t.Fatalf("firing meta: %+v", f)
	}
	if f.ActualValue == nil || *f.ActualValue != 80000 {
		t.Fatalf("actualValue: %v", f.ActualValue)
	}
	if f.CoinName == nil || *f.CoinName != "Bitcoin" {
		t.Fatalf("coinName: %v", f.CoinName)
	}
	if !f.Alert.FiredAt.Equal(now) {
		t.Fatalf("firedAt: %v", f.Alert.FiredAt)
	}
	if f.Alert.IsActive {
		t.Fatal("oneShot should have deactivated")
	}
	if len(repo.updates) != 1 {
		t.Fatalf("expected one UpdateState call, got %d", len(repo.updates))
	}
}

func TestTick_PriceCrossingAbove_DoesNotFire_BelowTarget(t *testing.T) {
	now := time.Now()
	d := dev("b")
	active := []storage.ActiveAlert{{DeviceID: d, Alert: domain.PriceAlert{
		ID:         alertID("under"),
		Condition:  domain.PriceCrossing{CoinID: "bitcoin", Direction: domain.Above, TargetPrice: 75000},
		Recurrence: domain.OneShot{},
		IsActive:   true,
	}}}
	coins := []domain.Coin{{ID: "bitcoin", CurrentPrice: 60000}}
	e, _, _ := newEval(active, nil, coins)

	firings, err := e.Tick(context.Background(), now)
	if err != nil {
		t.Fatalf("tick: %v", err)
	}
	if len(firings) != 0 {
		t.Fatalf("expected 0, got %d", len(firings))
	}
}

func TestTick_PercentChange_Above_24h_Fires(t *testing.T) {
	now := time.Now()
	d := dev("c")
	active := []storage.ActiveAlert{{DeviceID: d, Alert: domain.PriceAlert{
		ID:         alertID("p24"),
		Condition:  domain.PercentChange{CoinID: "eth", Direction: domain.Above, Window: domain.Window24h, Threshold: 5},
		Recurrence: domain.OneShot{},
		IsActive:   true,
	}}}
	coins := []domain.Coin{{ID: "eth", CurrentPrice: 0, PriceChangePercentage24h: ptrF(8.2)}}
	e, _, _ := newEval(active, nil, coins)

	firings, _ := e.Tick(context.Background(), now)
	if len(firings) != 1 {
		t.Fatalf("expected 1, got %d", len(firings))
	}
	if firings[0].ActualValue == nil || *firings[0].ActualValue != 8.2 {
		t.Fatalf("actualValue: %v", firings[0].ActualValue)
	}
}

func TestTick_PercentChange_30d_MissingField_Skips(t *testing.T) {
	now := time.Now()
	d := dev("d")
	active := []storage.ActiveAlert{{DeviceID: d, Alert: domain.PriceAlert{
		ID:         alertID("p30"),
		Condition:  domain.PercentChange{CoinID: "eth", Direction: domain.Above, Window: domain.Window30d, Threshold: 5},
		Recurrence: domain.OneShot{},
		IsActive:   true,
	}}}
	coins := []domain.Coin{{ID: "eth", CurrentPrice: 0 /* 30d not set */}}
	e, repo, _ := newEval(active, nil, coins)

	firings, _ := e.Tick(context.Background(), now)
	if len(firings) != 0 {
		t.Fatalf("expected 0, got %d", len(firings))
	}
	if len(repo.updates) != 0 {
		t.Fatalf("expected no state changes, got %d", len(repo.updates))
	}
}

func TestTick_PortfolioValue_Above_Fires_WithHoldings(t *testing.T) {
	now := time.Now()
	d := dev("e")
	id := alertID("pv")
	active := []storage.ActiveAlert{{DeviceID: d, Alert: domain.PriceAlert{
		ID:         id,
		Condition:  domain.PortfolioValue{Direction: domain.Above, Threshold: 100_000},
		Recurrence: domain.OneShot{},
		IsActive:   true,
	}}}
	holdings := map[uuid.UUID][]domain.Holding{d: {
		{CoinID: "bitcoin", Amount: 2, AverageBuyPrice: 30000, DateAdded: now},
	}}
	coins := []domain.Coin{{ID: "bitcoin", CurrentPrice: 60000}}
	e, _, _ := newEval(active, holdings, coins)

	firings, _ := e.Tick(context.Background(), now)
	if len(firings) != 1 {
		t.Fatalf("expected 1, got %d", len(firings))
	}
	if firings[0].ActualValue == nil || *firings[0].ActualValue != 120000 {
		t.Fatalf("actualValue: %v", firings[0].ActualValue)
	}
	if firings[0].CoinName != nil {
		t.Fatalf("expected nil coin name for portfolio variant, got %v", firings[0].CoinName)
	}
}

func TestTick_PortfolioValue_EmptyHoldings_DoesNotFire(t *testing.T) {
	now := time.Now()
	d := dev("f")
	active := []storage.ActiveAlert{{DeviceID: d, Alert: domain.PriceAlert{
		ID:         alertID("pv-empty"),
		Condition:  domain.PortfolioValue{Direction: domain.Below, Threshold: 100},
		Recurrence: domain.OneShot{},
		IsActive:   true,
	}}}
	e, repo, _ := newEval(active, nil, nil) // no holdings → skip
	firings, _ := e.Tick(context.Background(), now)
	if len(firings) != 0 {
		t.Fatalf("expected 0, got %d", len(firings))
	}
	if len(repo.updates) != 0 {
		t.Fatal("expected no state changes")
	}
}

func TestTick_PortfolioPnLPercent_Below_Fires(t *testing.T) {
	now := time.Now()
	d := dev("g")
	active := []storage.ActiveAlert{{DeviceID: d, Alert: domain.PriceAlert{
		ID:         alertID("pnl"),
		Condition:  domain.PortfolioPnLPercent{Direction: domain.Below, Threshold: -10},
		Recurrence: domain.OneShot{},
		IsActive:   true,
	}}}
	holdings := map[uuid.UUID][]domain.Holding{d: {
		// Bought 1 @ 100; now worth 80 → -20% P/L
		{CoinID: "bitcoin", Amount: 1, AverageBuyPrice: 100, DateAdded: now},
	}}
	coins := []domain.Coin{{ID: "bitcoin", CurrentPrice: 80}}
	e, _, _ := newEval(active, holdings, coins)

	firings, _ := e.Tick(context.Background(), now)
	if len(firings) != 1 {
		t.Fatalf("expected 1, got %d", len(firings))
	}
	if firings[0].ActualValue == nil || *firings[0].ActualValue != -20 {
		t.Fatalf("actualValue: %v", firings[0].ActualValue)
	}
}

func TestTick_Cooldown_DoesNotFire_BeforeInterval(t *testing.T) {
	now := time.Unix(1000, 0)
	pastFire := time.Unix(500, 0)
	d := dev("h")
	active := []storage.ActiveAlert{{DeviceID: d, Alert: domain.PriceAlert{
		ID:         alertID("cd-not"),
		Condition:  domain.PriceCrossing{CoinID: "bitcoin", Direction: domain.Above, TargetPrice: 100},
		Recurrence: domain.Cooldown{Seconds: 3600},
		IsActive:   true,
		FiredAt:    &pastFire,
	}}}
	coins := []domain.Coin{{ID: "bitcoin", CurrentPrice: 110}}
	e, _, _ := newEval(active, nil, coins)

	firings, _ := e.Tick(context.Background(), now)
	if len(firings) != 0 {
		t.Fatalf("expected 0, got %d", len(firings))
	}
}

func TestTick_Cooldown_Fires_AfterInterval_StaysActive(t *testing.T) {
	now := time.Unix(5000, 0)
	pastFire := time.Unix(500, 0)
	d := dev("i")
	id := alertID("cd-ok")
	active := []storage.ActiveAlert{{DeviceID: d, Alert: domain.PriceAlert{
		ID:         id,
		Condition:  domain.PriceCrossing{CoinID: "bitcoin", Direction: domain.Above, TargetPrice: 100},
		Recurrence: domain.Cooldown{Seconds: 3600},
		IsActive:   true,
		FiredAt:    &pastFire,
	}}}
	coins := []domain.Coin{{ID: "bitcoin", CurrentPrice: 110}}
	e, repo, _ := newEval(active, nil, coins)

	firings, _ := e.Tick(context.Background(), now)
	if len(firings) != 1 {
		t.Fatalf("expected 1, got %d", len(firings))
	}
	if !firings[0].Alert.IsActive {
		t.Fatal("cooldown should stay active across firings")
	}
	if firings[0].Alert.FiredAt == nil || !firings[0].Alert.FiredAt.Equal(now) {
		t.Fatalf("firedAt: %v", firings[0].Alert.FiredAt)
	}
	if len(repo.updates) != 1 {
		t.Fatalf("expected one update, got %d", len(repo.updates))
	}
}

func TestTick_OnCrossing_FalseToTrue_Fires(t *testing.T) {
	now := time.Now()
	d := dev("j")
	id := alertID("oc-flip")
	active := []storage.ActiveAlert{{DeviceID: d, Alert: domain.PriceAlert{
		ID:                  id,
		Condition:           domain.PriceCrossing{CoinID: "bitcoin", Direction: domain.Above, TargetPrice: 100},
		Recurrence:          domain.OnCrossing{},
		IsActive:            true,
		LastConditionResult: ptrB(false),
	}}}
	coins := []domain.Coin{{ID: "bitcoin", CurrentPrice: 110}}
	e, repo, _ := newEval(active, nil, coins)

	firings, _ := e.Tick(context.Background(), now)
	if len(firings) != 1 {
		t.Fatalf("expected 1, got %d", len(firings))
	}
	if firings[0].Alert.LastConditionResult == nil || !*firings[0].Alert.LastConditionResult {
		t.Fatalf("lcr: %v", firings[0].Alert.LastConditionResult)
	}
	if !firings[0].Alert.IsActive {
		t.Fatal("onCrossing should stay armed")
	}
	if len(repo.updates) != 1 {
		t.Fatalf("expected one update, got %d", len(repo.updates))
	}
}

func TestTick_OnCrossing_AlreadyTrue_DoesNotFire(t *testing.T) {
	now := time.Now()
	d := dev("k")
	active := []storage.ActiveAlert{{DeviceID: d, Alert: domain.PriceAlert{
		ID:                  alertID("oc-stable"),
		Condition:           domain.PriceCrossing{CoinID: "bitcoin", Direction: domain.Above, TargetPrice: 100},
		Recurrence:          domain.OnCrossing{},
		IsActive:            true,
		LastConditionResult: ptrB(true),
	}}}
	coins := []domain.Coin{{ID: "bitcoin", CurrentPrice: 110}}
	e, repo, _ := newEval(active, nil, coins)

	firings, _ := e.Tick(context.Background(), now)
	if len(firings) != 0 {
		t.Fatalf("expected 0, got %d", len(firings))
	}
	// State stayed true → no update should be persisted.
	if len(repo.updates) != 0 {
		t.Fatalf("expected no updates, got %d", len(repo.updates))
	}
}

func TestTick_SingleMarketsCall_PerPass_AnyMix(t *testing.T) {
	now := time.Now()
	d := dev("l")
	active := []storage.ActiveAlert{
		{DeviceID: d, Alert: domain.PriceAlert{
			ID: alertID("mix1"), Condition: domain.PriceCrossing{CoinID: "bitcoin", Direction: domain.Above, TargetPrice: 1}, Recurrence: domain.OneShot{}, IsActive: true,
		}},
		{DeviceID: d, Alert: domain.PriceAlert{
			ID: alertID("mix2"), Condition: domain.PercentChange{CoinID: "eth", Direction: domain.Above, Window: domain.Window24h, Threshold: 1}, Recurrence: domain.OneShot{}, IsActive: true,
		}},
		{DeviceID: d, Alert: domain.PriceAlert{
			ID: alertID("mix3"), Condition: domain.PortfolioValue{Direction: domain.Above, Threshold: 1}, Recurrence: domain.OneShot{}, IsActive: true,
		}},
	}
	holdings := map[uuid.UUID][]domain.Holding{d: {
		{CoinID: "doge", Amount: 1, AverageBuyPrice: 0.1, DateAdded: now},
	}}
	coins := []domain.Coin{
		{ID: "bitcoin", CurrentPrice: 2},
		{ID: "eth", PriceChangePercentage24h: ptrF(2)},
		{ID: "doge", CurrentPrice: 0.2},
	}
	e, _, markets := newEval(active, holdings, coins)

	_, _ = e.Tick(context.Background(), now)
	if markets.callCount != 1 {
		t.Fatalf("expected exactly 1 markets call, got %d", markets.callCount)
	}
	// Sorted union of needed coin ids.
	want := []string{"bitcoin", "doge", "eth"}
	if len(markets.lastIDs) != len(want) {
		t.Fatalf("expected ids %v, got %v", want, markets.lastIDs)
	}
	for i := range want {
		if markets.lastIDs[i] != want[i] {
			t.Fatalf("expected ids %v, got %v", want, markets.lastIDs)
		}
	}
}

func TestTick_ListActive_ErrorBubbles(t *testing.T) {
	e := &Evaluator{
		Alerts:   &fakeAlertRepo{failOn: "ListActive"},
		Holdings: &fakeHoldingRepo{},
		Markets:  &fakeMarkets{},
		Currency: "usd",
	}
	_, err := e.Tick(context.Background(), time.Now())
	if err == nil {
		t.Fatal("expected error to propagate")
	}
}

func TestTick_EmptyCurrency_Error(t *testing.T) {
	e := &Evaluator{Alerts: &fakeAlertRepo{}, Holdings: &fakeHoldingRepo{}, Markets: &fakeMarkets{}, Currency: ""}
	_, err := e.Tick(context.Background(), time.Now())
	if err == nil {
		t.Fatal("expected error for empty currency")
	}
}
