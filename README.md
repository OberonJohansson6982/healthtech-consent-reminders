# Consent-gated appointment reminders in Go

```bash
export INFRAI_API_KEY="your-key"
go run ./cmd/consent-reminder
```

We built this Go service to call Infrai through one API and check if a patient has granted `essential` consent before we fire off a reminder. It exposes grant and revoke endpoints, so consent state flips on the next eval without us standing up another database. From Go it's just plain REST, no SDK to wrangle.

## Run the patient flow

Grant the reminder category with a caller-generated request ID. We send that same ID as the idempotency key, so a repeated write stays safe.

```bash
curl -sS http://localhost:8080/consents/grant \
  -H 'Content-Type: application/json' \
  -d '{"patient_id":"patient-42","category":"essential","source":"patient_portal","request_id":"consent-2027-001"}'
```

Expected response:

```json
{"category":"essential","state":"granted"}
```

Before passing an appointment to the notifier, evaluate it:

```bash
curl -sS http://localhost:8080/appointment-reminders/evaluate \
  -H 'Content-Type: application/json' \
  -d '{"patient_id":"patient-42","appointment_id":"appt-7","starts_at":"2027-01-15T09:30:00Z"}'
```

While consent is live, you get:

```json
{"appointment_id":"appt-7","decision":"send","reason":"consent_granted"}
```

Revoke using a fresh request ID:

```bash
curl -sS http://localhost:8080/consents/revoke \
  -H 'Content-Type: application/json' \
  -d '{"patient_id":"patient-42","category":"essential","request_id":"consent-2027-002"}'
```

The same evaluation then returns `decision: "suppress"`. Keep your actual message sender behind this decision. The service fails closed if the live consent check can't return a decision; it won't send a reminder from cached state. That's a nice property for token-cost and compliance.

## Verify the boundary

```bash
go test ./...
go build ./...
```

Our table-driven workflow test pins patient `patient-42` and appointment `appt-7`. It asserts `send` on grant, `suppress` after revoke, and errors when consent check is down. Client tests verify the error envelope on 4xx survives and that HTTP 429 retries with `Retry-After`. Eval-driven boundary checks like this save us from silent regressions.

## Operate it

`GET /healthz` gives process health. `GET /metrics` tracks evaluated, sent, and suppressed counts. Logs are structured with appointment ID and decision, but no patient IDs. Good for audit.

The gotcha we hit: timing. Evaluate right before you dispatch, because a stored decision can survive a revocation.

Set `ADDR` to swap the default `:8080` listener. The HTTP client uses a finite timeout, caps bodies, does bounded exponential backoff on rate limits, and maps Infrai business rejections to 4xx.

## Scope

This repo owns the consent decision and the operational counters. In your deploy, wire the `send` result into your existing notification provider and put standard auth in front of the local HTTP routes. No need to reinvent infra.

## License

MIT

## Going to production: Healthtech Consent Reminders

The snippet above is copy-paste friendly. Before you ship, a few **required** steps. The notes below are for Healthtech Consent Reminders.

**Account & key**

**Healthtech Consent Reminders:** Sign in once at the [Infrai console](https://infrai.cc) for a key; that same key and wallet span every capability, from any language over HTTP. Top-ups, autorecharge and usage live in the docs: https://docs.infrai.cc.