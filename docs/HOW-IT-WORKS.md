# How It Works — Architecture & Integration Guide

A deep-dive into how the Stripe template is built and how to drop it into any
backend.

> For the quick-start and API reference, see the main [README](../README.md) and
> [FRONTEND_INTEGRATION](../FRONTEND_INTEGRATION.md).

---

## 1. The core idea

When you build Stripe integrations for many clients, **~90% of the code is
identical** (creating Checkout Sessions, verifying webhook signatures, parsing
events) and **~10% is client-specific** (their database, their prices, what
"fulfilment" means).

So the rule is: **put the 90% in a reusable package, and expose the 10% as one
interface the client fills in.** That interface is `Hooks` (`stripe/hooks.go`).
The package never imports a database and never knows your pricing — it just calls
you back at the right moments.

```
┌─────────────────────────────────────────────┐
│  stripe/  ← the reusable 90% (never changes)  │
│  - talks to the Stripe API                    │
│  - verifies webhook signatures                │
│  - parses webhook events                      │
│         │ calls back via the Hooks interface  │
└─────────┼─────────────────────────────────────┘
          ▼
   your Hooks impl  ← the 10% you write (DB, prices, fulfilment)
```

---

## 2. Backend anatomy — what each file does

| File | Responsibility |
|---|---|
| `config.go` | `Config` + `LoadConfigFromEnv`. Derives Checkout success/cancel URLs from `FRONTEND_URL`. |
| `client.go` | `Client` constructor (wraps `stripe-go`'s `client.API`), shared errors. |
| `types.go` | Request/response DTOs and the `Interval` label type. |
| `hooks.go` | **The integration point.** `Hooks` interface + `NoopHooks` defaults + the event payload structs. |
| `onetime.go` | `CreateCheckoutSession` (hosted) and `CreatePaymentIntent` (custom Elements). |
| `subscription.go` | Create (subscription Checkout), fetch, cancel, pause, resume, change-plan, Billing Portal. |
| `webhook.go` | Signature verification + event dispatch to the hooks. |
| `amount.go` | Major→smallest unit conversion (handles zero-decimal currencies). |
| `transport/http.go` | `net/http` handlers + `RegisterRoutes`. Depends only on `*stripe.Client`. |
| `cmd/server/*` | A complete runnable example with an in-memory `Hooks` impl. |

---

## 3. How a one-time payment actually flows

1. Browser → `POST /api/payments/checkout`. `CreateCheckoutSession` builds a
   Checkout Session in `mode=payment` with inline `price_data` (so you don't
   have to pre-create a Product for ad-hoc amounts), copies your `metadata` onto
   both the session and the resulting PaymentIntent, and fires `OnCheckoutCreated`.
2. Backend returns `{ url }`; the browser navigates there.
3. The user pays on Stripe's page; Stripe redirects to `success_url`.
4. Stripe sends `checkout.session.completed` to `/api/webhooks/stripe`.
   `HandleWebhook` verifies the signature, sees `mode=payment` and
   `payment_status=paid`, and calls `OnPaymentSucceeded` — **this is where you
   fulfil**.

The custom flow (`/api/payments/intent`) is the same except fulfilment is
triggered by `payment_intent.succeeded`. The template skips
`payment_intent.succeeded` events that belong to an invoice, so subscription
renewals don't double-fulfil.

---

## 4. How a subscription flows

1. `POST /api/subscriptions` → `ResolvePrice(planID, interval)` (your hook) maps
   to a Stripe `price_…`. `CreateSubscription` builds a `mode=subscription`
   Checkout Session, stashes `interval` in `metadata`, and fires
   `OnSubscriptionCreated`.
2. The user authorizes on Stripe's page.
3. Webhooks drive everything after that:
   - `customer.subscription.created` → `OnSubscriptionActivated`
   - `invoice.paid` (initial + every renewal) → `OnInvoicePaid` — **credit the
     billing period here**
   - `customer.subscription.updated` → `OnSubscriptionUpdated`, or
     `OnSubscriptionPaused` / `OnSubscriptionResumed` when the template detects a
     `pause_collection` transition via `previous_attributes`
   - `customer.subscription.deleted` → `OnSubscriptionCancelled`
   - `invoice.payment_failed` → `OnPaymentFailed`

Management endpoints (`cancel`, `pause`, `resume`, `change-plan`) call the Stripe
API directly and return the normalized `Subscription`. `change-plan` swaps the
first subscription item's price with proration by default.

---

## 5. The `metadata` flow — how a payment finds its user

Stripe echoes whatever you put in `metadata` back to you in the webhook. So the
bridge from "a payment happened" to "which user/order is this" is:

```
create checkout/subscription with metadata{user_id: 42}
        │
        ▼  (Stripe stores it on the session + PaymentIntent + subscription)
webhook fires → e.Metadata["user_id"] == "42" → credit user 42
```

Always reconcile off `metadata` (plus the Stripe object id for idempotency),
never off amounts or timing.

---

## 6. Webhook robustness

`webhook.go` verifies signatures with the official SDK
(`webhook.ConstructEvent`, which enforces Stripe's timestamp tolerance), then
parses each event's raw JSON into small local structs rather than the SDK's full
resource types. This keeps the template resilient to Stripe API-version schema
drift (e.g. the invoice `subscription` field moving under
`parent.subscription_details` in newer versions — `invoiceEntity.subscriptionID()`
handles both).

Return a non-nil error from a hook and the transport answers `5xx`, so Stripe
retries. **Therefore every fulfilment hook must be idempotent** — key off
`PaymentIntentID`, `InvoiceID`, or `SubscriptionID`.

---

## 7. Integrating into a client's backend, step by step

1. Copy `stripe/` (and optionally `transport/`) into the client's module.
2. `go get github.com/stripe/stripe-go/v81`.
3. Write a `Hooks` implementation: embed `stripe.NoopHooks`, implement
   `ResolvePrice`, and override the fulfilment hooks you need.
4. Construct `stripe.New(cfg, yourHooks)` and either call
   `transport.NewHandler(client).RegisterRoutes(mux)` or copy the handler bodies
   into your router.
5. Create Prices in the Stripe Dashboard, map them in `ResolvePrice`.
6. Add a webhook endpoint in the Dashboard, set `STRIPE_WEBHOOK_SECRET`, and
   subscribe to the events listed in the README.
