package consent

import (
	"context"
	"errors"
	"testing"
)

type stubChecker struct {
	granted  bool
	err      error
	category string
}

func (s *stubChecker) Granted(_ context.Context, _ string, category string) (bool, error) {
	s.category = category
	return s.granted, s.err
}

func TestEvaluateReminder(t *testing.T) {
	tests := []struct {
		name         string
		granted      bool
		checkErr     error
		wantDecision string
		wantReason   string
		wantErr      bool
	}{
		{name: "granted reminder is released", granted: true, wantDecision: "send", wantReason: "consent_granted"},
		{name: "revoked reminder is suppressed", granted: false, wantDecision: "suppress", wantReason: "consent_not_granted"},
		{name: "check error fails closed", checkErr: errors.New("check unavailable"), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checker := &stubChecker{granted: tt.granted, err: tt.checkErr}
			got, err := EvaluateReminder(context.Background(), checker, AppointmentReminder{
				PatientID: "patient-42", AppointmentID: "appt-7", StartsAt: "2027-01-15T09:30:00Z",
			})
			if (err != nil) != tt.wantErr {
				t.Fatalf("EvaluateReminder() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if got.Decision != tt.wantDecision || got.Reason != tt.wantReason {
				t.Fatalf("EvaluateReminder() = %#v, want decision=%q reason=%q", got, tt.wantDecision, tt.wantReason)
			}
			if checker.category != AppointmentReminderCategory {
				t.Fatalf("category = %q, want %q", checker.category, AppointmentReminderCategory)
			}
		})
	}
}
