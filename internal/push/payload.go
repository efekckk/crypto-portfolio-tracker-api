// Package push owns the APNs delivery client and the payload formatter that
// turns an evaluator firing into a notification body.
package push

import (
	"fmt"
	"strconv"
	"time"

	"github.com/efekckk/crypto-portfolio-tracker-api/internal/domain"
	"github.com/efekckk/crypto-portfolio-tracker-api/internal/eval"
	"github.com/efekckk/crypto-portfolio-tracker-api/internal/storage"
)

// Payload builds the JSON map APNs accepts for a single firing. The shape
// matches what the iOS RemoteNotificationDelegate decodes: aps.alert is a
// dict, sound is "default", thread-id is the alert id, and a handful of
// custom fields ride alongside so the client can sync local state.
func Payload(f eval.Firing, d storage.Device) map[string]any {
	title := localizedTitle(d.Locale)
	body := bodyForFiring(f, d.Locale)
	payload := map[string]any{
		"aps": map[string]any{
			"alert": map[string]any{
				"title": title,
				"body":  body,
			},
			"sound":     "default",
			"thread-id": f.Alert.ID,
		},
		"alert_id":  f.Alert.ID,
		"fired_at":  f.FiredAt.UTC().Format(time.RFC3339),
	}
	if f.ActualValue != nil {
		payload["actual_value"] = *f.ActualValue
	}
	if f.CoinName != nil {
		payload["coin_name"] = *f.CoinName
	}
	if _, isOnCrossing := f.Alert.Recurrence.(domain.OnCrossing); isOnCrossing {
		if f.Alert.LastConditionResult != nil {
			payload["new_last_condition_result"] = *f.Alert.LastConditionResult
		}
	}
	return payload
}

func localizedTitle(locale string) string {
	if locale == "tr" {
		return "Uyarı"
	}
	return "Alert"
}

// bodyForFiring matches the iOS AlertNotificationFormatter per-variant copy.
// Uses actual value when present, threshold as fallback.
func bodyForFiring(f eval.Firing, locale string) string {
	tr := locale == "tr"
	switch c := f.Alert.Condition.(type) {
	case domain.PriceCrossing:
		name := coinDisplayName(f.CoinName, c.CoinID)
		amount := formatMoney(valueOrThreshold(f.ActualValue, c.TargetPrice))
		if f.ActualValue != nil {
			if tr {
				return fmt.Sprintf("%s %s değerine ulaştı", name, amount)
			}
			return fmt.Sprintf("%s reached %s", name, amount)
		}
		if tr {
			return fmt.Sprintf("%s %s değerini geçti", name, amount)
		}
		return fmt.Sprintf("%s crossed %s", name, amount)

	case domain.PercentChange:
		name := coinDisplayName(f.CoinName, c.CoinID)
		pct := formatPercent(valueOrThreshold(f.ActualValue, c.Threshold))
		window := windowLabel(c.Window, tr)
		if f.ActualValue != nil {
			if tr {
				return fmt.Sprintf("%s, %s %s içinde hareket etti", name, pct, window)
			}
			return fmt.Sprintf("%s moved %s in %s", name, pct, window)
		}
		if tr {
			return fmt.Sprintf("%s, %s eşiğini %s içinde geçti", name, pct, window)
		}
		return fmt.Sprintf("%s crossed %s in %s", name, pct, window)

	case domain.PortfolioValue:
		amount := formatMoney(valueOrThreshold(f.ActualValue, c.Threshold))
		if tr {
			return fmt.Sprintf("Portföy toplamı %s değerine ulaştı", amount)
		}
		return fmt.Sprintf("Portfolio total reached %s", amount)

	case domain.PortfolioPnLPercent:
		pct := formatPercent(valueOrThreshold(f.ActualValue, c.Threshold))
		if tr {
			return fmt.Sprintf("Portföy K/Z şimdi %s", pct)
		}
		return fmt.Sprintf("Portfolio P/L is now %s", pct)
	}
	return ""
}

func coinDisplayName(coinName *string, coinID string) string {
	if coinName != nil && *coinName != "" {
		return *coinName
	}
	if coinID == "" {
		return ""
	}
	return capitalize(coinID)
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	first := s[0]
	if first >= 'a' && first <= 'z' {
		first = first - 'a' + 'A'
	}
	return string(first) + s[1:]
}

func valueOrThreshold(actual *float64, threshold float64) float64 {
	if actual != nil {
		return *actual
	}
	return threshold
}

func formatMoney(v float64) string {
	// Keep the body small; integer dollars are fine for showcase copy. Avoids
	// dragging a full locale-aware currency formatter into the backend.
	return "$" + strconv.FormatFloat(v, 'f', 0, 64)
}

func formatPercent(v float64) string {
	s := strconv.FormatFloat(v, 'f', -1, 64)
	return s + "%"
}

func windowLabel(w domain.PercentWindow, tr bool) string {
	switch w {
	case domain.Window24h:
		if tr {
			return "24s"
		}
		return "24h"
	case domain.Window7d:
		if tr {
			return "7g"
		}
		return "7d"
	case domain.Window30d:
		if tr {
			return "30g"
		}
		return "30d"
	}
	return ""
}
