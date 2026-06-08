package api

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/efekckk/crypto-portfolio-tracker-api/internal/domain"
)

// alertDTO is the wire shape of an alert in API requests and responses.
// condition and recurrence are kept as RawMessage so we can hand them to the
// domain package's discriminated-union helpers without double-decoding.
type alertDTO struct {
	ID                  string          `json:"id"`
	Condition           json.RawMessage `json:"condition"`
	Recurrence          json.RawMessage `json:"recurrence"`
	IsActive            bool            `json:"is_active"`
	FiredAt             *time.Time      `json:"fired_at,omitempty"`
	LastConditionResult *bool           `json:"last_condition_result,omitempty"`
}

// alertsListResponse wraps the list endpoint payload.
type alertsListResponse struct {
	Alerts []alertDTO `json:"alerts"`
}

// toDomain decodes the DTO's union fields into a domain.PriceAlert. The
// alert.ID is left as a string (UUID validation is the handler's job).
func (d alertDTO) toDomain() (domain.PriceAlert, error) {
	if len(d.Condition) == 0 {
		return domain.PriceAlert{}, fmt.Errorf("condition is required")
	}
	if len(d.Recurrence) == 0 {
		return domain.PriceAlert{}, fmt.Errorf("recurrence is required")
	}
	cond, err := domain.UnmarshalAlertCondition(d.Condition)
	if err != nil {
		return domain.PriceAlert{}, fmt.Errorf("condition: %w", err)
	}
	rec, err := domain.UnmarshalRecurrence(d.Recurrence)
	if err != nil {
		return domain.PriceAlert{}, fmt.Errorf("recurrence: %w", err)
	}
	return domain.PriceAlert{
		ID:                  d.ID,
		Condition:           cond,
		Recurrence:          rec,
		IsActive:            d.IsActive,
		FiredAt:             d.FiredAt,
		LastConditionResult: d.LastConditionResult,
	}, nil
}

// alertToDTO encodes a domain alert into its wire shape.
func alertToDTO(a domain.PriceAlert) (alertDTO, error) {
	cond, err := domain.MarshalAlertCondition(a.Condition)
	if err != nil {
		return alertDTO{}, fmt.Errorf("marshal condition: %w", err)
	}
	rec, err := domain.MarshalRecurrence(a.Recurrence)
	if err != nil {
		return alertDTO{}, fmt.Errorf("marshal recurrence: %w", err)
	}
	return alertDTO{
		ID:                  a.ID,
		Condition:           cond,
		Recurrence:          rec,
		IsActive:            a.IsActive,
		FiredAt:             a.FiredAt,
		LastConditionResult: a.LastConditionResult,
	}, nil
}
