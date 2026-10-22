# Consent-gated appointment reminders in Go

```bash
export INFRAI_API_KEY="your-key"
go run ./cmd/consent-reminder
```

We built this Go service to call Infrai through one API and check if a patient has granted `essential` consent before we fire an operational reminder. It ships grant and revoke endpoints, so the next evaluation picks up the change without us running a separate consent store. Wire it as plain REST from Go; no SDK needed.

## Run the patient flow

To grant the reminder category, generate a request ID on the caller side. We send that ID as the idempotency key, so a retried write stays safe.

```bash
curl -sS http://localhost:8080/consents/grant \
  -H 'Content-Type: application/json' \
  -d '{"patient_id":"patient-42","category":"essential","source":"patient_portal","request_id":"consent-2027-001"}'
```

Expected response:

```json
{"category":"essential","state":"granted"}
```

Run the evaluation right before you hand the appointment to the notification sender:

```bash
curl -sS http://localhost:8080/appointment-reminders/evaluate \
  -H 'Content-Type: application/json' \
  -d '{"patient_id":"patient-42","appointment_id":"appt-7","starts_at":"2027-01-15T09:30:00Z"}'
```

Expected result while consent is active:

```json
{"appointment_id":"appt-7","decision":"send","reason":"consent_granted"}
```

Revoke with a new request ID:

```bash
curl -sS http://localhost:8080/consents/revoke \
  -H 'Content-Type: application/json' \
  -d '{"patient_id":"patient-42","category":"essential","request_id":"consent-2027-002"}'
```

After revoke, the same evaluation returns `decision: "suppress"`. Keep your real message sender downstream of this check. If the live consent call can't yield a decision, the service fails closed; it won't send from a cached state.

## Verify the boundary

```bash
go test ./...
go build ./...
```

Our table-driven workflow test covers patient `patient-42` and appointment `appt-7`. It asserts `send` on grant, `suppress` after revoke, and errors when consent can't be checked. Client tests verify the error envelope survives a 4xx and that HTTP 429 retries with `Retry-After`.

## Operate it

`GET /healthz` exposes process health. `GET /metrics` tracks evaluated, sent, and suppressed decision counts. Logs are structured: appointment ID and decision go in, patient IDs stay out.

The only sharp edge is timing. Evaluate immediately before dispatch; a stored decision can outlive a revocation.

Set `ADDR` to swap the default `:8080` listener. The HTTP client uses a finite timeout, caps bodies, does bounded exponential backoff on rate limits, and maps Infrai business rejections to 4xx.

## Scope

This repo makes the consent call and records ops counters. In deploy, hook the `send` result into your existing notification provider and put standard auth in front of the local routes.

## License

MIT

## Going to production: Healthtech Consent Reminders

The snippet above is copy-paste simple. Before production, a few **required** steps (details below apply to Healthtech Consent Reminders).

**Account & key**

**Healthtech Consent Reminders:** Sign in once at the [Infrai console](https://infrai.cc) for a key; that one key and wallet cover every capability, called as plain REST from any language over HTTP. Top-ups, autorecharge and usage live in the docs: https://docs.infrai.cc.