package ratelimiter

import (
	"testing"
	"time"
)

func TestEnoughTimeHasPassed_PositiveInputs(t *testing.T) {
	window := 60 * time.Second

	tests := []struct {
		expected bool
		name     string
		first    int64
		last     int64
	}{
		/* These inputs should return without errors */
		{true, "well over window", 100, 300},
		{false, "under window", 100, 110},
		{true, "exactly at window", 100, 160},
		{false, "one second short", 100, 159},
		{false, "exactly the same moment", 100, 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, err := hasTimePassed(tt.first, tt.last, window)

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if ok != tt.expected {
				t.Errorf("got %v, want %v", ok, tt.expected)
			}
		})
	}
}

func TestEnoughTimeHasPassed_ErroneousInputs(t *testing.T) {
	window := 60 * time.Second

	tests := []struct {
		name     string
		first    int64
		last     int64
	}{
		/* These inputs should return with errors and false return value */
		{"last before first", 300, 100},
		{"last hit is 0", 100, 0},
		{"first hit is 0", 0, 100},
		{"last hit is -200", 100, -200},
		{"first hit is -200", -200, 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, err := hasTimePassed(tt.first, tt.last, window)

			if err == nil {
				t.Fatal("expected an error, got nil")
			}

			if ok {
				t.Error("expected false alongside the error, got true")
			}
		})
	}
}
