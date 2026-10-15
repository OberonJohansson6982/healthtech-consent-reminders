package consent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGrantSendsAuthenticatedIdempotentWrite(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}
		if r.URL.Path != "/v1/auth/consent/grant/patient-42" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("authorization header = %q", r.Header.Get("Authorization"))
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		want := map[string]string{
			"user_id": "patient-42", "category": AppointmentReminderCategory,
			"source": "patient_portal", "idempotency_key": "consent-001",
		}
		for key, value := range want {
			if body[key] != value {
				t.Errorf("body[%q] = %q, want %q", key, body[key], value)
			}
		}
		_, _ = w.Write([]byte(`{"ok":true,"data":{},"error":null,"metadata":{}}`))
	}))
	defer server.Close()

	client, _ := NewClient("test-key")
	client.baseURL = server.URL
	if err := client.Grant(context.Background(), "patient-42", AppointmentReminderCategory, "patient_portal", "consent-001"); err != nil {
		t.Fatalf("Grant() error = %v", err)
	}
}

func TestGrantedDecodesBusinessRejectionBeforeStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"ok":false,"data":null,"error":{"code":"BAD_CATEGORY","message":"category rejected"},"metadata":{}}`))
	}))
	defer server.Close()

	client, _ := NewClient("test-key")
	client.baseURL = server.URL
	_, err := client.Granted(context.Background(), "patient-42", AppointmentReminderCategory)
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("Granted() error = %T %v, want *APIError", err, err)
	}
	if apiErr.Code != "BAD_CATEGORY" || apiErr.HTTPStatus != http.StatusBadRequest {
		t.Fatalf("APIError = %#v", apiErr)
	}
}

func TestGrantedRetriesRateLimit(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		if requests == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"ok":false,"data":null,"error":{"code":"RATE_LIMITED","message":"retry later"},"metadata":{}}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"data":{"granted":true},"error":null,"metadata":{}}`))
	}))
	defer server.Close()

	client, _ := NewClient("test-key")
	client.baseURL = server.URL
	granted, err := client.Granted(context.Background(), "patient-42", AppointmentReminderCategory)
	if err != nil || !granted {
		t.Fatalf("Granted() = %v, %v", granted, err)
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want 2", requests)
	}
}
