package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/efekckk/crypto-portfolio-tracker-api/internal/domain"
	"github.com/efekckk/crypto-portfolio-tracker-api/internal/eval"
	"github.com/efekckk/crypto-portfolio-tracker-api/internal/push"
	"github.com/efekckk/crypto-portfolio-tracker-api/internal/storage"
)

type fakeEvaluator struct {
	firings []eval.Firing
	err     error
}

func (f *fakeEvaluator) Tick(_ context.Context, _ time.Time) ([]eval.Firing, error) {
	return f.firings, f.err
}

type fakeFirings struct {
	nextID   int64
	inserted []storage.Firing
	statuses map[int64]storage.DeliveryStatus
}

func newFakeFirings() *fakeFirings {
	return &fakeFirings{statuses: map[int64]storage.DeliveryStatus{}}
}

func (f *fakeFirings) Insert(_ context.Context, fi storage.Firing) (int64, error) {
	f.nextID++
	f.inserted = append(f.inserted, fi)
	f.statuses[f.nextID] = storage.DeliveryPending
	return f.nextID, nil
}

func (f *fakeFirings) UpdateStatus(_ context.Context, id int64, status storage.DeliveryStatus) error {
	f.statuses[id] = status
	return nil
}

type fakeDevices struct {
	devices map[uuid.UUID]*storage.Device
	deleted []uuid.UUID
}

func (f *fakeDevices) Get(_ context.Context, id uuid.UUID) (*storage.Device, error) {
	d, ok := f.devices[id]
	if !ok {
		return nil, storage.ErrDeviceNotFound
	}
	return d, nil
}

func (f *fakeDevices) Delete(_ context.Context, id uuid.UUID) error {
	f.deleted = append(f.deleted, id)
	delete(f.devices, id)
	return nil
}

type fakePusher struct {
	resultFor map[uuid.UUID]push.DeliveryStatus
	errFor    map[uuid.UUID]error
	calls     int
}

func (f *fakePusher) Push(_ context.Context, firing eval.Firing, _ storage.Device) (push.DeliveryStatus, error) {
	f.calls++
	if err := f.errFor[firing.DeviceID]; err != nil {
		return f.resultFor[firing.DeviceID], err
	}
	if s, ok := f.resultFor[firing.DeviceID]; ok {
		return s, nil
	}
	return push.Delivered, nil
}

func mkFiring(devID uuid.UUID, alertID string) eval.Firing {
	return eval.Firing{
		DeviceID: devID,
		Alert: domain.PriceAlert{
			ID:         alertID,
			Condition:  domain.PriceCrossing{CoinID: "btc", Direction: domain.Above, TargetPrice: 1},
			Recurrence: domain.OneShot{},
			IsActive:   false,
		},
		FiredAt: time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC),
	}
}

func newRunner(firings []eval.Firing, devices map[uuid.UUID]*storage.Device, pushResults map[uuid.UUID]push.DeliveryStatus) (*Runner, *fakeFirings, *fakeDevices, *fakePusher) {
	frec := newFakeFirings()
	devs := &fakeDevices{devices: devices}
	pusher := &fakePusher{resultFor: pushResults, errFor: map[uuid.UUID]error{}}
	r := &Runner{
		Evaluator: &fakeEvaluator{firings: firings},
		Firings:   frec,
		Devices:   devs,
		Pusher:    pusher,
	}
	return r, frec, devs, pusher
}

func TestTick_NoFirings_ReturnsZero(t *testing.T) {
	r, frec, _, pusher := newRunner(nil, nil, nil)
	count, err := r.Tick(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("tick: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0, got %d", count)
	}
	if pusher.calls != 0 {
		t.Fatalf("expected no pushes, got %d", pusher.calls)
	}
	if len(frec.inserted) != 0 {
		t.Fatalf("expected no inserts, got %d", len(frec.inserted))
	}
}

func TestTick_DeliveredFiring_FullPath(t *testing.T) {
	dev := uuid.New()
	alert := uuid.NewString()
	firings := []eval.Firing{mkFiring(dev, alert)}
	devices := map[uuid.UUID]*storage.Device{
		dev: {DeviceID: dev, APNsToken: "tok", APNsEnv: "development", Locale: "en"},
	}
	r, frec, devs, pusher := newRunner(firings, devices, map[uuid.UUID]push.DeliveryStatus{dev: push.Delivered})

	count, err := r.Tick(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("tick: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1, got %d", count)
	}
	if pusher.calls != 1 {
		t.Fatalf("expected 1 push, got %d", pusher.calls)
	}
	if len(frec.inserted) != 1 {
		t.Fatalf("expected 1 firing row, got %d", len(frec.inserted))
	}
	if frec.statuses[1] != storage.DeliveryDelivered {
		t.Fatalf("expected delivered, got %s", frec.statuses[1])
	}
	if len(devs.deleted) != 0 {
		t.Fatalf("expected no device deletions, got %d", len(devs.deleted))
	}
}

func TestTick_DroppedFiring_RevokesDevice(t *testing.T) {
	dev := uuid.New()
	firings := []eval.Firing{mkFiring(dev, uuid.NewString())}
	devices := map[uuid.UUID]*storage.Device{
		dev: {DeviceID: dev, APNsToken: "tok", APNsEnv: "production", Locale: "en"},
	}
	r, frec, devs, _ := newRunner(firings, devices, map[uuid.UUID]push.DeliveryStatus{dev: push.Dropped})

	_, err := r.Tick(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("tick: %v", err)
	}
	if frec.statuses[1] != storage.DeliveryDropped {
		t.Fatalf("expected dropped status, got %s", frec.statuses[1])
	}
	if len(devs.deleted) != 1 || devs.deleted[0] != dev {
		t.Fatalf("expected device %s deleted, got %v", dev, devs.deleted)
	}
}

func TestTick_FailedFiring_DoesNotRevokeDevice(t *testing.T) {
	dev := uuid.New()
	firings := []eval.Firing{mkFiring(dev, uuid.NewString())}
	devices := map[uuid.UUID]*storage.Device{
		dev: {DeviceID: dev, APNsToken: "tok", APNsEnv: "development", Locale: "en"},
	}
	r, frec, devs, _ := newRunner(firings, devices, map[uuid.UUID]push.DeliveryStatus{dev: push.Failed})

	_, err := r.Tick(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("tick: %v", err)
	}
	if frec.statuses[1] != storage.DeliveryFailed {
		t.Fatalf("expected failed status, got %s", frec.statuses[1])
	}
	if len(devs.deleted) != 0 {
		t.Fatalf("expected no device deletions, got %d", len(devs.deleted))
	}
}

func TestTick_MissingDevice_MarksFailed_NoPush(t *testing.T) {
	dev := uuid.New()
	firings := []eval.Firing{mkFiring(dev, uuid.NewString())}
	// no device seeded → Devices.Get returns ErrDeviceNotFound
	r, frec, _, pusher := newRunner(firings, nil, nil)

	_, err := r.Tick(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("tick: %v", err)
	}
	if frec.statuses[1] != storage.DeliveryFailed {
		t.Fatalf("expected failed status, got %s", frec.statuses[1])
	}
	if pusher.calls != 0 {
		t.Fatalf("expected no pushes when device missing, got %d", pusher.calls)
	}
}

func TestTick_PushNetworkError_StillMarksFailed(t *testing.T) {
	dev := uuid.New()
	firings := []eval.Firing{mkFiring(dev, uuid.NewString())}
	devices := map[uuid.UUID]*storage.Device{
		dev: {DeviceID: dev, APNsToken: "tok", APNsEnv: "development", Locale: "en"},
	}
	r, frec, devs, pusher := newRunner(firings, devices, map[uuid.UUID]push.DeliveryStatus{dev: push.Failed})
	pusher.errFor = map[uuid.UUID]error{dev: errors.New("network down")}

	_, err := r.Tick(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("tick: %v", err)
	}
	if frec.statuses[1] != storage.DeliveryFailed {
		t.Fatalf("expected failed status, got %s", frec.statuses[1])
	}
	if len(devs.deleted) != 0 {
		t.Fatalf("network failure should not revoke device")
	}
}

func TestTick_InvalidAlertID_Skipped(t *testing.T) {
	dev := uuid.New()
	firings := []eval.Firing{mkFiring(dev, "not-a-uuid")}
	devices := map[uuid.UUID]*storage.Device{
		dev: {DeviceID: dev, APNsToken: "tok", APNsEnv: "development", Locale: "en"},
	}
	r, frec, _, pusher := newRunner(firings, devices, nil)

	count, err := r.Tick(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("tick: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 (count returned regardless of skip), got %d", count)
	}
	if len(frec.inserted) != 0 {
		t.Fatalf("invalid alert id should skip insert, got %d", len(frec.inserted))
	}
	if pusher.calls != 0 {
		t.Fatalf("invalid alert id should skip push, got %d", pusher.calls)
	}
}

func TestTick_EvaluatorError_Bubbles(t *testing.T) {
	r := &Runner{
		Evaluator: &fakeEvaluator{err: errors.New("boom")},
		Firings:   newFakeFirings(),
		Devices:   &fakeDevices{},
		Pusher:    &fakePusher{},
	}
	_, err := r.Tick(context.Background(), time.Now())
	if err == nil {
		t.Fatal("expected error to propagate")
	}
}

func TestMapStatus_PerVariant(t *testing.T) {
	cases := []struct {
		in   push.DeliveryStatus
		want storage.DeliveryStatus
	}{
		{push.Delivered, storage.DeliveryDelivered},
		{push.Dropped, storage.DeliveryDropped},
		{push.Failed, storage.DeliveryFailed},
		{push.DeliveryStatus("unknown"), storage.DeliveryFailed},
	}
	for _, c := range cases {
		if got := mapStatus(c.in); got != c.want {
			t.Fatalf("mapStatus(%q): expected %q, got %q", c.in, c.want, got)
		}
	}
}
