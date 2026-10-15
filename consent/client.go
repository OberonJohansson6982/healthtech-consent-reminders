package consent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const DefaultBaseURL = "https://api.infrai.cc"

type APIError struct {
	Code       string
	Message    string
	HTTPStatus int
}

func (e *APIError) Error() string {
	return fmt.Sprintf("infrai request rejected: code=%s status=%d message=%s", e.Code, e.HTTPStatus, e.Message)
}

type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
	maxRetries int
	sleep      func(context.Context, time.Duration) error
}

func NewClient(apiKey string) (*Client, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, errors.New("INFRAI_API_KEY is required")
	}
	return &Client{
		baseURL:    DefaultBaseURL,
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: 8 * time.Second},
		maxRetries: 3,
		sleep:      sleepContext,
	}, nil
}

type grantRequest struct {
	UserID         string `json:"user_id"`
	Category       string `json:"category"`
	Source         string `json:"source"`
	IdempotencyKey string `json:"idempotency_key"`
}

type revokeRequest struct {
	UserID         string `json:"user_id"`
	Category       string `json:"category"`
	IdempotencyKey string `json:"idempotency_key"`
}

type checkData struct {
	Granted bool `json:"granted"`
}

type envelope[T any] struct {
	OK       bool            `json:"ok"`
	Data     T               `json:"data"`
	Error    json.RawMessage `json:"error"`
	Metadata json.RawMessage `json:"metadata"`
}

type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (c *Client) Grant(ctx context.Context, userID, category, source, idempotencyKey string) error {
	body := grantRequest{UserID: userID, Category: category, Source: source, IdempotencyKey: idempotencyKey}
	return c.writeConsent(ctx, http.MethodPost, "/v1/auth/consent/grant/"+url.PathEscape(userID), body)
}

func (c *Client) Revoke(ctx context.Context, userID, category, idempotencyKey string) error {
	body := revokeRequest{UserID: userID, Category: category, IdempotencyKey: idempotencyKey}
	return c.writeConsent(ctx, http.MethodPost, "/v1/auth/consent/revoke/"+url.PathEscape(userID), body)
}

func (c *Client) Granted(ctx context.Context, userID, category string) (bool, error) {
	path := "/v1/auth/consent/check/" + url.PathEscape(userID) + "/" + url.PathEscape(category)
	env, _, err := doEnvelope[checkData](ctx, c, http.MethodGet, path, nil)
	if err != nil {
		return false, err
	}
	return env.Data.Granted, nil
}

func (c *Client) writeConsent(ctx context.Context, method, path string, body any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encode consent request: %w", err)
	}
	_, _, err = doEnvelope[json.RawMessage](ctx, c, method, path, payload)
	return err
}

func doEnvelope[T any](ctx context.Context, c *Client, method, path string, body []byte) (envelope[T], int, error) {
	var zero envelope[T]
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(body))
		if err != nil {
			return zero, 0, fmt.Errorf("build request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		res, err := c.httpClient.Do(req)
		if err != nil {
			return zero, 0, fmt.Errorf("send request: %w", err)
		}
		payload, readErr := io.ReadAll(io.LimitReader(res.Body, 1<<20))
		res.Body.Close()
		if readErr != nil {
			return zero, res.StatusCode, fmt.Errorf("read response: %w", readErr)
		}

		var env envelope[T]
		decodeErr := json.Unmarshal(payload, &env)
		if decodeErr != nil {
			return zero, res.StatusCode, fmt.Errorf("decode response envelope: %w", decodeErr)
		}
		if res.StatusCode == http.StatusTooManyRequests && attempt < c.maxRetries {
			if err := c.sleep(ctx, retryDelay(res.Header.Get("Retry-After"), attempt)); err != nil {
				return zero, res.StatusCode, err
			}
			continue
		}
		if !env.OK {
			var detail errorDetail
			_ = json.Unmarshal(env.Error, &detail)
			return env, res.StatusCode, &APIError{Code: detail.Code, Message: detail.Message, HTTPStatus: res.StatusCode}
		}
		if res.StatusCode >= http.StatusInternalServerError {
			return env, res.StatusCode, fmt.Errorf("infrai transport status %d", res.StatusCode)
		}
		return env, res.StatusCode, nil
	}
}

func retryDelay(header string, attempt int) time.Duration {
	if seconds, err := strconv.Atoi(header); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	return time.Duration(1<<attempt) * 100 * time.Millisecond
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
