# Stripe Payment Template (Go)

A **drop-in, reusable Stripe integration** for one-time payments **and**
subscriptions. The backend is a dependency-light Go package (only the official
`stripe-go` SDK) that you copy into any Go backend.

The whole thing is driven by a single **`Hooks` interface** — the package handles
everything Stripe-specific (Checkout Sessions, Payment Intents, subscriptions,
the Billing Portal, signature verification, webhooks) and calls your code
wherever your app needs to do something (resolve a price, fulfil a payment,
credit a user). There is **no database, no pricing table, and no business logic**
baked in, so you can copy it into any project.

> This is the Stripe sibling of the Razorpay template; it follows the same
> "reusable 90% + a `Hooks` interface for the 10%" design so the two are
> interchangeable conceptually.

```
stripe-template/
├── stripe/             # core Go package (copy this into any Go backend)
│   ├── config.go       # Config + LoadConfigFromEnv
│   ├── client.go       # Client constructor + shared errors
│   ├── types.go        # request/response types
│   ├── hooks.go        # ← THE integration point (Hooks + NoopHooks)
│   ├── onetime.go      # CreateCheckoutSession, CreatePaymentIntent
│   ├── subscription.go # create / cancel / pause / resume / change-plan / portal / fetch
│   ├── webhook.go      # signature check + event dispatch to Hooks
│   └── amount.go       # major→smallest unit conversion
├── transport/http.go   # net/http handlers + RegisterRoutes (framework-agnostic)
├── cmd/server/main.go  # runnable example wiring an in-memory Hooks impl
├── .env.example
├── FRONTEND_INTEGRATION.md
├── docs/HOW-IT-WORKS.md
└── go.mod
```

---

## 1. How payments flow

### One-time payment (Stripe Checkout — recommended)
```
Browser: pay()              Go backend                         Stripe
  │  POST /api/payments/checkout ───────────► CreateCheckoutSession ─► Checkout API
  │  ◄─────── { url, session } ◄───────────── OnCheckoutCreated (hook: persist pending)
  │  window.location = url   (Stripe-hosted page)
  │  ── user pays on Stripe ─────────────────────────────────►
  │  ◄── redirect to success_url
  │
  └─ meanwhile, Stripe → POST /api/webhooks/stripe → HandleWebhook
                                              └► OnPaymentSucceeded (hook: FULFIL)
```
**Fulfilment happens in the webhook, not on the success redirect.** The success
page is just UX. The webhook (`checkout.session.completed`) is the source of
truth and is retried by Stripe until you return 2xx — so make your hooks
idempotent.

### One-time payment (custom UI with Stripe Elements)
```
POST /api/payments/intent ──► CreatePaymentIntent ──► returns { client_secret }
   browser confirms client_secret with Stripe Elements
   Stripe → webhook payment_intent.succeeded → OnPaymentSucceeded (FULFIL)
```

### Subscription
```
Browser: subscribe()        Go backend                         Stripe
  │  POST /api/subscriptions ──────────────► CreateSubscription
  │                                          ├─ ResolvePrice(planId, interval) (hook)
  │                                          └─ OnSubscriptionCreated (hook: persist)
  │  ◄────── { url, session }
  │  window.location = url   (Stripe-hosted subscription Checkout)
  │  ── user authorizes the mandate ─────────────────────────►
  │
  └─ Stripe → webhooks → customer.subscription.created / invoice.paid / .updated / .deleted
                          └► OnSubscriptionActivated / OnInvoicePaid / … (hooks: FULFIL)
```

---

## 2. Backend setup

### Install / run
```bash
cp .env.example .env     # fill in your keys
go mod tidy
go run ./cmd/server      # listens on :8080
```

### Configure
Set the env vars from `.env.example`:

| Variable | Purpose |
|---|---|
| `STRIPE_SECRET_KEY` | Secret API key (Dashboard → Developers → API keys) |
| `STRIPE_PUBLISHABLE_KEY` | Public key sent to the browser |
| `STRIPE_WEBHOOK_SECRET` | Webhook signing secret (`whsec_…`) |
| `STRIPE_CURRENCY` | Default currency for ad-hoc Checkout / intents (default `usd`) |
| `STRIPE_SUCCESS_URL` / `STRIPE_CANCEL_URL` | Checkout redirect targets (optional; derived from `FRONTEND_URL`) |
| `FRONTEND_URL` | Billing Portal return + fallback to derive Checkout URLs |
| `STRIPE_PRO_*_PRICE_ID` | Your `plan:interval` → Stripe Price id mapping (example hooks) |

### Implement the `Hooks` interface — the only thing you must write

```go
type MyHooks struct {
    stripe.NoopHooks // inherit no-op defaults; override what you need
    db *sql.DB
}

// 1) Map your plans to Stripe Price ids (price_...).
func (h MyHooks) ResolvePrice(ctx context.Context, planID string, interval stripe.Interval) (string, error) {
    var priceID string
    err := h.db.QueryRowContext(ctx,
        "SELECT stripe_price_id FROM plans WHERE code=$1 AND interval=$2",
        planID, interval).Scan(&priceID)
    if err != nil || priceID == "" {
        return "", stripe.ErrPriceNotResolved
    }
    return priceID, nil
}

// 2) Persist the pending checkout so you can reconcile it later.
func (h MyHooks) OnCheckoutCreated(ctx context.Context, e stripe.CheckoutCreated) error {
    _, err := h.db.ExecContext(ctx,
        "INSERT INTO checkouts(session_id, amount, currency, user_id, status) VALUES($1,$2,$3,$4,'pending')",
        e.SessionID, e.Amount, e.Currency, e.Metadata["user_id"])
    return err
}

// 3) Fulfil on the webhook (IDEMPOTENT — Stripe retries).
func (h MyHooks) OnPaymentSucceeded(ctx context.Context, e stripe.PaymentSucceeded) error {
    res, err := h.db.ExecContext(ctx,
        "UPDATE checkouts SET status='paid', payment_intent=$1 WHERE session_id=$2 AND status<>'paid'",
        e.PaymentIntentID, e.SessionID)
    if err != nil { return err }
    if n, _ := res.RowsAffected(); n == 0 { return nil } // already handled
    // ... credit the user, send receipt, etc.
    return nil
}

func (h MyHooks) OnInvoicePaid(ctx context.Context, e stripe.InvoicePaid) error {
    // credit the new billing period (idempotent on e.InvoiceID)
    return nil
}
```

Wire it up:

```go
cfg, _ := stripe.LoadConfigFromEnv()
client := stripe.New(cfg, MyHooks{db: db})

mux := http.NewServeMux()
transport.NewHandler(client).RegisterRoutes(mux)
http.ListenAndServe(":8080", mux)
```

`cmd/server/main.go` + `cmd/server/hooks.go` is a complete, runnable version
using an in-memory example implementation.

### Endpoints registered by `RegisterRoutes`

| Method & path | Body | Purpose |
|---|---|---|
| `POST /api/payments/checkout` | `{amount, currency?, product_name?, customer_email?, metadata?}` | one-time Checkout Session (hosted) |
| `POST /api/payments/intent` | `{amount, currency?, customer_email?, metadata?}` | PaymentIntent for custom Elements UI |
| `POST /api/subscriptions` | `{plan_id, interval, customer_email?, trial_days?, metadata?}` | subscription Checkout Session |
| `POST /api/subscriptions/cancel` | `{subscription_id, cancel_at_period_end}` | cancel (now or at period end) |
| `POST /api/subscriptions/pause` | `{subscription_id}` | pause collection |
| `POST /api/subscriptions/resume` | `{subscription_id}` | resume collection |
| `POST /api/subscriptions/change-plan` | `{subscription_id, new_plan_id, interval, prorate?}` | upgrade/downgrade |
| `POST /api/subscriptions/portal` | `{customer_id, return_url?}` | Billing Portal session (self-service) |
| `GET  /api/subscriptions/{id}` | – | fetch live status |
| `POST /api/webhooks/stripe` | Stripe event | webhook receiver |
| `GET  /api/config` | – | returns public `publishable_key` for the frontend |

> Uses Go 1.22+ `http.ServeMux` method+path patterns. On older Go or another
> router (chi/gin/echo), copy the handler bodies — they only depend on
> `stripe.Client`.

### Webhook configuration
In the Dashboard → Developers → Webhooks, point an endpoint at
`https://your-domain/api/webhooks/stripe`, copy its signing secret into
`STRIPE_WEBHOOK_SECRET`, and subscribe to:
`checkout.session.completed`, `payment_intent.succeeded`,
`payment_intent.payment_failed`, `invoice.paid`, `invoice.payment_failed`,
`customer.subscription.created`, `customer.subscription.updated`,
`customer.subscription.deleted` (and optionally
`customer.subscription.paused` / `.resumed`).

**Local testing** with the Stripe CLI:
```bash
stripe listen --forward-to localhost:8080/api/webhooks/stripe
# copy the printed whsec_... into STRIPE_WEBHOOK_SECRET, then:
stripe trigger checkout.session.completed
```

---

## 3. Frontend setup

You do **not** need a frontend package — your app talks to this backend over
plain HTTP/JSON. See [`FRONTEND_INTEGRATION.md`](./FRONTEND_INTEGRATION.md) for
every endpoint, payload, and copy-pasteable browser snippets.

The short version for hosted Checkout (one-time **and** subscriptions):
```js
const res = await fetch("/api/payments/checkout", {
  method: "POST",
  headers: { "Content-Type": "application/json" },
  body: JSON.stringify({ amount: 49, product_name: "Pro pack", metadata: { user_id: "42" } }),
});
const { url } = await res.json();
window.location = url;            // Stripe-hosted page; it redirects back when done
```

For a custom card form, use `/api/payments/intent` + `@stripe/stripe-js` +
Stripe Elements with the returned `client_secret` (snippet in the frontend doc).

---

## 4. Security checklist
- Treat the **webhook as the source of truth** for fulfilment; the success-page
  redirect is just UX confirmation and can be spoofed.
- Keep `STRIPE_SECRET_KEY` and `STRIPE_WEBHOOK_SECRET` server-side only. The
  browser only ever receives the publishable key.
- Verify every webhook signature (this template does, with Stripe's timestamp
  tolerance, in `VerifyWebhookSignature` / `HandleWebhook`).
- Make every webhook hook **idempotent** — Stripe retries until you 2xx. Key off
  `PaymentIntentID` / `InvoiceID` / `SubscriptionID`.
- Lock down CORS (the example uses `*` for local dev only).

---

## 5. Stripe vs. Razorpay — what's different
- **No client-side signature verify step.** Razorpay returns a signature to the
  browser that you re-verify; Stripe's hosted Checkout simply redirects to your
  `success_url`. Fulfilment is webhook-only.
- **`metadata` replaces `notes`** — same idea: anything you put there comes back
  in the webhooks, so stash user/order ids to reconcile.
- **Prices, not plans.** A Stripe *Price* (`price_…`) encodes amount + cadence;
  `ResolvePrice` maps your internal `plan:interval` to one.
- **Billing Portal.** Stripe gives you a hosted self-service page
  (`/api/subscriptions/portal`) for customers to manage cards/cancellations —
  there's no Razorpay equivalent.

## 6. What was intentionally left out
This is a *pure payments* template. It deliberately omits credits/wallets,
coupons, emails, and user management — those belong in your `Hooks`
implementation. The `metadata` map carries whatever identifiers you need to
bridge back to your domain.
