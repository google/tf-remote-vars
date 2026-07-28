# 0006 — Backend Webhook Delay Logic

**What to build:**
Implement webhook propagation delays in the backend server. When a variable updates and triggers downstream stack webhooks, the server sleeps for the configured number of minutes in the background before sending the HTTP POST webhook request.

**Blocked by:** 0005 — Webhook Delay Proto and Storage Schema

**Status:** ready-for-agent

- [x] Server `RegisterNamespace` API maps gRPC request `webhook_delay_minutes` to the namespace store.
- [x] Server `GetNamespace` API returns `webhook_delay_minutes`.
- [x] `propagateChange` triggers webhooks inside asynchronous goroutines that block for `ns.WebhookDelayMinutes` using `s.clock.Sleep`.
- [x] A new test `TestWebhookDelayPropagation` verifies the delay logic using `clockwork.FakeClock` to advance time without sleeping real-time threads.
