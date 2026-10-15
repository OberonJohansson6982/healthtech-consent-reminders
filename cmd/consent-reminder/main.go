package main

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/infrai-examples/healthtech-consent-reminders/consent"
)

type service struct {
	client     *consent.Client
	evaluated  atomic.Uint64
	sent       atomic.Uint64
	suppressed atomic.Uint64
}

type consentChange struct {
	PatientID string `json:"patient_id"`
	Category  string `json:"category"`
	Source    string `json:"source,omitempty"`
	RequestID string `json:"request_id"`
}

func main() {
	client, err := consent.NewClient(os.Getenv("INFRAI_API_KEY"))
	if err != nil {
		slog.Error("configuration rejected", "error", err)
		os.Exit(1)
	}

	svc := &service{client: client}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /consents/grant", svc.grant)
	mux.HandleFunc("POST /consents/revoke", svc.revoke)
	mux.HandleFunc("POST /appointment-reminders/evaluate", svc.evaluate)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /metrics", svc.metrics)

	server := &http.Server{
		Addr:              envOr("ADDR", ":8080"),
		Handler:           requestLog(mux),
		ReadHeaderTimeout: 3 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	slog.Info("consent reminder service listening", "addr", server.Addr)
	if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		slog.Error("service stopped", "error", err)
		os.Exit(1)
	}
}

func (s *service) grant(w http.ResponseWriter, r *http.Request) {
	var change consentChange
	if !decodeJSON(w, r, &change) || !validChange(w, change) {
		return
	}
	if strings.TrimSpace(change.Source) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "source is required"})
		return
	}
	if err := s.client.Grant(r.Context(), change.PatientID, change.Category, change.Source, change.RequestID); err != nil {
		writeUpstreamError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"state": "granted", "category": change.Category})
}

func (s *service) revoke(w http.ResponseWriter, r *http.Request) {
	var change consentChange
	if !decodeJSON(w, r, &change) || !validChange(w, change) {
		return
	}
	if err := s.client.Revoke(r.Context(), change.PatientID, change.Category, change.RequestID); err != nil {
		writeUpstreamError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"state": "revoked", "category": change.Category})
}

func (s *service) evaluate(w http.ResponseWriter, r *http.Request) {
	var reminder consent.AppointmentReminder
	if !decodeJSON(w, r, &reminder) {
		return
	}
	decision, err := consent.EvaluateReminder(r.Context(), s.client, reminder)
	if err != nil {
		writeUpstreamError(w, err)
		return
	}
	s.evaluated.Add(1)
	if decision.Decision == "send" {
		s.sent.Add(1)
	} else {
		s.suppressed.Add(1)
	}
	slog.Info("appointment reminder evaluated", "appointment_id", decision.AppointmentID, "decision", decision.Decision, "reason", decision.Reason)
	writeJSON(w, http.StatusOK, decision)
}

func (s *service) metrics(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]uint64{
		"reminders_evaluated":  s.evaluated.Load(),
		"reminders_sent":       s.sent.Load(),
		"reminders_suppressed": s.suppressed.Load(),
	})
}

func validChange(w http.ResponseWriter, change consentChange) bool {
	if change.PatientID == "" || change.Category == "" || change.RequestID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "patient_id, category, and request_id are required"})
		return false
	}
	return true
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return false
	}
	return true
}

func writeUpstreamError(w http.ResponseWriter, err error) {
	var apiErr *consent.APIError
	if errors.As(err, &apiErr) && apiErr.HTTPStatus >= 400 && apiErr.HTTPStatus < 500 {
		writeJSON(w, apiErr.HTTPStatus, map[string]string{"error": apiErr.Code, "message": apiErr.Message})
		return
	}
	slog.Error("consent dependency request failed", "error", err)
	writeJSON(w, http.StatusBadGateway, map[string]string{"error": "consent_dependency_error"})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func requestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		next.ServeHTTP(w, r)
		slog.Info("request complete", "method", r.Method, "path", r.URL.Path, "duration_ms", time.Since(started).Milliseconds())
	})
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
