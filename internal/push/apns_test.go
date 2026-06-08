package push

import (
	"testing"
)

func TestNewAPNsPusher_RejectsEmptyKey(t *testing.T) {
	_, err := NewAPNsPusher(nil, "K", "T", "B")
	if err == nil {
		t.Fatal("expected error for empty key")
	}
}

func TestNewAPNsPusher_RejectsMissingMetadata(t *testing.T) {
	// Use a stub PEM that AuthKeyFromBytes will accept — we want to test that
	// metadata validation fails BEFORE we get there. Empty metadata short-
	// circuits the constructor before AuthKeyFromBytes is called.
	dummy := []byte("not a real key — should not be parsed")
	if _, err := NewAPNsPusher(dummy, "", "T", "B"); err == nil {
		t.Fatal("expected error for empty key id")
	}
	if _, err := NewAPNsPusher(dummy, "K", "", "B"); err == nil {
		t.Fatal("expected error for empty team id")
	}
	if _, err := NewAPNsPusher(dummy, "K", "T", ""); err == nil {
		t.Fatal("expected error for empty bundle id")
	}
}

func TestNewAPNsPusher_RejectsInvalidKey(t *testing.T) {
	_, err := NewAPNsPusher([]byte("not a real key"), "K", "T", "B")
	if err == nil {
		t.Fatal("expected error for invalid PEM")
	}
}

func TestDeliveryStatus_Constants(t *testing.T) {
	if Delivered != "delivered" || Failed != "failed" || Dropped != "dropped" {
		t.Fatal("unexpected delivery status constant value")
	}
}
