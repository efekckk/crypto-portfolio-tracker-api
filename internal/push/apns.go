package push

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/sideshow/apns2"
	"github.com/sideshow/apns2/token"

	"github.com/efekckk/crypto-portfolio-tracker-api/internal/eval"
	"github.com/efekckk/crypto-portfolio-tracker-api/internal/storage"
)

// DeliveryStatus mirrors storage.DeliveryStatus values returned by Push.
type DeliveryStatus string

const (
	Delivered DeliveryStatus = "delivered"
	Failed    DeliveryStatus = "failed"
	Dropped   DeliveryStatus = "dropped" // token invalid / 410 Gone
)

// Pusher sends one firing to APNs and reports the resulting delivery state.
// The cron worker calls this once per firing; tests inject a fake.
type Pusher interface {
	Push(ctx context.Context, firing eval.Firing, device storage.Device) (DeliveryStatus, error)
}

// APNsPusher delivers via Apple Push Notification service.
type APNsPusher struct {
	BundleID string
	dev      *apns2.Client
	prod     *apns2.Client
}

// NewAPNsPusher builds a Pusher backed by a JWT-authenticated apns2 client.
// Pass the .p8 contents (PEM-encoded), the Key ID, the Team ID, and the
// app's bundle id. The same auth is used for dev and prod, only the endpoint
// differs (chosen per-firing from Device.APNsEnv).
func NewAPNsPusher(authKeyPEM []byte, keyID, teamID, bundleID string) (*APNsPusher, error) {
	if len(authKeyPEM) == 0 {
		return nil, errors.New("push: APNs auth key is empty")
	}
	if keyID == "" || teamID == "" || bundleID == "" {
		return nil, errors.New("push: keyID, teamID, and bundleID are required")
	}
	authKey, err := token.AuthKeyFromBytes(authKeyPEM)
	if err != nil {
		return nil, fmt.Errorf("push: parse APNs key: %w", err)
	}
	tok := &token.Token{
		AuthKey: authKey,
		KeyID:   keyID,
		TeamID:  teamID,
	}
	return &APNsPusher{
		BundleID: bundleID,
		dev:      apns2.NewTokenClient(tok).Development(),
		prod:     apns2.NewTokenClient(tok).Production(),
	}, nil
}

// Push sends one firing. Returns Delivered on success, Dropped on 410 Gone
// (token revoked — caller wipes the device row), Failed otherwise.
func (p *APNsPusher) Push(ctx context.Context, firing eval.Firing, device storage.Device) (DeliveryStatus, error) {
	body, err := json.Marshal(Payload(firing, device))
	if err != nil {
		return Failed, fmt.Errorf("push: marshal payload: %w", err)
	}
	notif := &apns2.Notification{
		DeviceToken: device.APNsToken,
		Topic:       p.BundleID,
		Payload:     json.RawMessage(body),
	}

	client := p.prod
	if device.APNsEnv == "development" {
		client = p.dev
	}
	resp, err := client.PushWithContext(ctx, notif)
	if err != nil {
		return Failed, fmt.Errorf("push: APNs send: %w", err)
	}
	switch resp.StatusCode {
	case 200:
		return Delivered, nil
	case 410:
		return Dropped, fmt.Errorf("push: APNs 410 (token revoked): %s", resp.Reason)
	default:
		return Failed, fmt.Errorf("push: APNs %s: %s", strconv.Itoa(resp.StatusCode), resp.Reason)
	}
}
