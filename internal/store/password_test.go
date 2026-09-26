package store_test

import (
	"errors"
	"testing"

	"skilltree/internal/store"
)

func TestValidatePassword(t *testing.T) {
	if err := store.ValidatePassword("short"); !errors.Is(err, store.ErrWeakPassword) {
		t.Fatalf("expected weak, got %v", err)
	}
	if err := store.ValidatePassword("longenough"); err != nil {
		t.Fatalf("expected ok, got %v", err)
	}
}
