package consent

import (
	"context"
	"fmt"
)

const AppointmentReminderCategory = "essential"

type Checker interface {
	Granted(ctx context.Context, userID, category string) (bool, error)
}

type AppointmentReminder struct {
	PatientID     string `json:"patient_id"`
	AppointmentID string `json:"appointment_id"`
	StartsAt      string `json:"starts_at"`
}

type ReminderDecision struct {
	AppointmentID string `json:"appointment_id"`
	Decision      string `json:"decision"`
	Reason        string `json:"reason"`
}

func EvaluateReminder(ctx context.Context, checker Checker, reminder AppointmentReminder) (ReminderDecision, error) {
	if reminder.PatientID == "" || reminder.AppointmentID == "" || reminder.StartsAt == "" {
		return ReminderDecision{}, fmt.Errorf("patient_id, appointment_id, and starts_at are required")
	}
	granted, err := checker.Granted(ctx, reminder.PatientID, AppointmentReminderCategory)
	if err != nil {
		return ReminderDecision{}, fmt.Errorf("check appointment reminder consent: %w", err)
	}
	if !granted {
		return ReminderDecision{AppointmentID: reminder.AppointmentID, Decision: "suppress", Reason: "consent_not_granted"}, nil
	}
	return ReminderDecision{AppointmentID: reminder.AppointmentID, Decision: "send", Reason: "consent_granted"}, nil
}
