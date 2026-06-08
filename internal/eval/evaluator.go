package eval

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/efekckk/crypto-portfolio-tracker-api/internal/domain"
	"github.com/efekckk/crypto-portfolio-tracker-api/internal/storage"
)

// Firing is the controller-side record of an alert firing — exactly what the
// push pipeline needs to assemble an APNs payload.
type Firing struct {
	DeviceID    uuid.UUID
	Alert       domain.PriceAlert
	FiredAt     time.Time
	ActualValue *float64
	CoinName    *string // nil for portfolio variants or when the coin isn't in the markets response
}

// AlertRepo is the subset of storage.AlertRepo the evaluator needs.
type AlertRepo interface {
	ListActive(ctx context.Context) ([]storage.ActiveAlert, error)
	UpdateState(ctx context.Context, id uuid.UUID, isActive bool, firedAt *time.Time, lastCondResult *bool) error
}

// HoldingRepo is the subset of storage.HoldingRepo the evaluator needs.
type HoldingRepo interface {
	ListByDevice(ctx context.Context, deviceID uuid.UUID) ([]domain.Holding, error)
}

// Evaluator runs a single tick of the cron loop.
type Evaluator struct {
	Alerts   AlertRepo
	Holdings HoldingRepo
	Markets  MarketsClient
	Currency string // e.g. "usd"
}

// Tick performs one evaluation pass and returns the firings that should be
// delivered. State (firedAt/isActive/lastConditionResult) is persisted before
// returning.
func (e *Evaluator) Tick(ctx context.Context, now time.Time) ([]Firing, error) {
	if e == nil {
		return nil, errors.New("eval: nil Evaluator")
	}
	if e.Currency == "" {
		return nil, errors.New("eval: Currency is empty")
	}
	active, err := e.Alerts.ListActive(ctx)
	if err != nil {
		return nil, fmt.Errorf("eval: list active: %w", err)
	}
	if len(active) == 0 {
		return nil, nil
	}

	coinIDSet := map[string]struct{}{}
	needsPortfolio := map[uuid.UUID]struct{}{}
	for _, aa := range active {
		if ids := aa.Alert.Condition.RequiredCoinIDs(); ids != nil {
			for _, id := range ids {
				coinIDSet[id] = struct{}{}
			}
		} else {
			needsPortfolio[aa.DeviceID] = struct{}{}
		}
	}

	holdingsByDevice := map[uuid.UUID][]domain.Holding{}
	for devID := range needsPortfolio {
		holds, err := e.Holdings.ListByDevice(ctx, devID)
		if err != nil {
			return nil, fmt.Errorf("eval: list holdings: %w", err)
		}
		holdingsByDevice[devID] = holds
		for _, h := range holds {
			coinIDSet[h.CoinID] = struct{}{}
		}
	}

	var coinIDs []string
	for id := range coinIDSet {
		coinIDs = append(coinIDs, id)
	}
	sort.Strings(coinIDs)

	var coins []domain.Coin
	if len(coinIDs) > 0 {
		coins, err = e.Markets.FetchMarkets(ctx, coinIDs, e.Currency)
		if err != nil {
			return nil, fmt.Errorf("eval: fetch markets: %w", err)
		}
	}
	coinsByID := map[string]domain.Coin{}
	for _, c := range coins {
		coinsByID[c.ID] = c
	}

	var firings []Firing
	for _, aa := range active {
		_, isPortfolioDevice := needsPortfolio[aa.DeviceID]
		var summary *portfolioSummary
		if isPortfolioDevice {
			holds := holdingsByDevice[aa.DeviceID]
			if len(holds) > 0 {
				s := buildSummary(holds, coinsByID)
				summary = &s
			}
		}

		evalRes, ok := evaluate(aa.Alert.Condition, coinsByID, summary)
		if !ok {
			continue
		}
		updated := aa.Alert
		stateChanged := false
		if _, isOnCrossing := aa.Alert.Recurrence.(domain.OnCrossing); isOnCrossing {
			b := evalRes.conditionTrue
			updated.LastConditionResult = &b
			if !boolEqualPtr(aa.Alert.LastConditionResult, &b) {
				stateChanged = true
			}
		}
		if shouldFire(aa.Alert, evalRes.conditionTrue, now) {
			firedAt := now
			updated.FiredAt = &firedAt
			if _, isOneShot := aa.Alert.Recurrence.(domain.OneShot); isOneShot {
				updated.IsActive = false
			}
			stateChanged = true
			firings = append(firings, Firing{
				DeviceID:    aa.DeviceID,
				Alert:       updated,
				FiredAt:     firedAt,
				ActualValue: evalRes.actualValue,
				CoinName:    coinName(aa.Alert.Condition, coinsByID),
			})
		}
		if stateChanged {
			id, err := uuid.Parse(updated.ID)
			if err != nil {
				return nil, fmt.Errorf("eval: invalid alert id %q: %w", updated.ID, err)
			}
			if err := e.Alerts.UpdateState(ctx, id, updated.IsActive, updated.FiredAt, updated.LastConditionResult); err != nil {
				return nil, fmt.Errorf("eval: update state: %w", err)
			}
		}
	}
	return firings, nil
}

// boolEqualPtr is true when both pointers are nil, or both non-nil and equal.
func boolEqualPtr(a, b *bool) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}
