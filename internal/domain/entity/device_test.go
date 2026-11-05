package entity

import (
	"testing"
	"time"
)

func TestDeviceReclaimDeadlineIsThreeDays(t *testing.T) {
	registered := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	verified := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name         string
		lastVerified *time.Time
		want         time.Time
	}{
		{"before first log", nil, time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC)},
		{"after verified log", &verified, time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)},
	} {
		t.Run(test.name, func(t *testing.T) {
			deadline := DeviceReclaimableAt(registered, test.lastVerified)
			if !deadline.Equal(test.want) {
				t.Fatalf("deadline=%s want %s", deadline, test.want)
			}
			for _, boundary := range []struct {
				now     time.Time
				allowed bool
			}{
				{test.want.Add(-time.Nanosecond), false}, {test.want, true}, {test.want.Add(time.Nanosecond), true},
			} {
				if allowed := !boundary.now.Before(deadline); allowed != boundary.allowed {
					t.Fatalf("at %s allowed=%v", boundary.now, allowed)
				}
			}
		})
	}
}

func TestIMEIValidation(t *testing.T) {
	for _, input := range []string{"490154203237518", " 000000000094821 "} {
		if _, err := NormalizeIMEI(input); err != nil {
			t.Errorf("valid IMEI rejected: %q", input)
		}
	}
	for _, input := range []string{"4821", "000000000000000", "490154203237519", "49015420323751a", "４９０１５４２０３２３７５１８"} {
		if _, err := NormalizeIMEI(input); err == nil {
			t.Errorf("invalid IMEI accepted: %q", input)
		}
	}
}
