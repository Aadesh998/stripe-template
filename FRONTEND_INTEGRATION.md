# Frontend Integration Guide

Your frontend talks to this Go backend over plain HTTP/JSON. This doc lists every
endpoint, the JSON payload to send, and the response you get back.

Base URL = wherever the server runs (default `http://localhost:8080`).
All request bodies are JSON and `POST` unless noted.

There are two ways to collect a one-time payment:
1. **Stripe Checkout (hosted)** — simplest. You redirect to a Stripe-hosted page.
   No Stripe JS required.
2. **Stripe Elements (custom)** — you build the card form in your page with
   `@stripe/stripe-js` and confirm a PaymentIntent's `client_secret`.

Subscriptions always use hosted Checkout here.

---

## The hosted one-time payment flow (recommended)

```
[1] POST /api/payments/checkout  ──►  backend creates a Checkout Session, returns { url }
[2] window.location = url         ──►  user pays on Stripe's hosted page
[3] Stripe redirects to success_url
[4] Stripe → POST /api/webhooks/stripe → backend FULFILS (source of truth)
```

Actual fulfilment (crediting the user / marking paid) happens server-side in the
**webhook**, not on the success redirect. The redirect just shows a thank-you page.

---

## Endpoints

### `GET /api/config`
Get the public publishable key (safe in the browser).

**Response**
```json
{ "publishable_key": "pk_test_xxxxxxxx" }
```

---

### `POST /api/payments/checkout` — one-time hosted Checkout

**Request payload**
```json
{
  "amount": 49,                       // MAJOR unit (e.g. dollars), not cents
  "currency": "usd",                  // optional, defaults to server config
  "product_name": "Pro pack",         // shown on the Stripe page
  "description": "One-time purchase",  // optional
  "quantity": 1,                      // optional, defaults to 1
  "customer_email": "user@example.com",// optional, prefilled on the page
  "success_url": "https://app/success?session_id={CHECKOUT_SESSION_ID}", // optional
  "cancel_url": "https://app/cancel", // optional
  "metadata": { "user_id": "42" }      // optional, echoed back in the webhook
}
```
> Put any id you'll need later in `metadata` — it comes back verbatim in webhooks.

**Response**
```json
{
  "url": "https://checkout.stripe.com/c/pay/cs_test_...",
  "publishable_key": "pk_test_xxxxxxxx",
  "session": {
    "id": "cs_test_...",
    "url": "https://checkout.stripe.com/c/pay/cs_test_...",
    "mode": "payment",
    "status": "open",
    "currency": "usd"
  }
}
```

**Then redirect the browser:**
```js
const res = await fetch("/api/payments/checkout", {
  method: "POST",
  headers: { "Content-Type": "application/json" },
  body: JSON.stringify({ amount: 49, product_name: "Pro pack", metadata: { user_id: "42" } }),
});
const { url } = await res.json();
window.location = url;   // Stripe-hosted page; redirects to success_url when done
```

---

### `POST /api/payments/intent` — custom card form (Stripe Elements)

Use this when you want the card form inside your own page.

**Request payload**
```json
{
  "amount": 49,
  "currency": "usd",
  "description": "Pro pack",
  "customer_email": "user@example.com",
  "metadata": { "user_id": "42" }
}
```

**Response**
```json
{
  "client_secret": "pi_3Nxxxx_secret_xxxx",
  "publishable_key": "pk_test_xxxxxxxx",
  "payment_intent": { "id": "pi_3Nxxxx", "status": "requires_payment_method", "amount": 4900, "currency": "usd" }
}
```

**Then confirm it in the browser** (needs `@stripe/stripe-js`):
```js
import { loadStripe } from "@stripe/stripe-js";

const { client_secret, publishable_key } = await (await fetch("/api/payments/intent", {
  method: "POST", headers: { "Content-Type": "application/json" },
  body: JSON.stringify({ amount: 49, metadata: { user_id: "42" } }),
})).json();

const stripe = await loadStripe(publishable_key);
const elements = stripe.elements({ clientSecret: client_secret });
const paymentElement = elements.create("payment");
paymentElement.mount("#payment-element");

// on submit:
const { error } = await stripe.confirmPayment({
  elements,
  confirmParams: { return_url: "https://your-app/success" },
});
if (error) showError(error.message);
// success → Stripe fires payment_intent.succeeded to your webhook (fulfilment)
```

---

### `POST /api/subscriptions` — create a subscription (hosted Checkout)

**Request payload**
```json
{
  "plan_id": "pro",            // YOUR internal plan id (mapped server-side to a Price)
  "interval": "monthly",       // monthly | quarterly | yearly
  "customer_email": "user@example.com",
  "trial_days": 0,             // optional free-trial length
  "success_url": "https://app/welcome?session_id={CHECKOUT_SESSION_ID}", // optional
  "cancel_url": "https://app/pricing", // optional
  "metadata": { "user_id": "42" }
}
```
> `plan_id` is **your** identifier (e.g. `"pro"`). The backend maps
> `plan_id:interval` to the real Stripe Price id. If it can't resolve, you get
> `400`. Make sure the server's price env vars are set (see `.env.example`).

**Response** — same shape as `/payments/checkout`: redirect to `url`.
```json
{
  "url": "https://checkout.stripe.com/c/pay/cs_test_...",
  "publishable_key": "pk_test_xxxxxxxx",
  "session": { "id": "cs_test_...", "mode": "subscription", "status": "open" }
}
```

```js
const { url } = await (await fetch("/api/subscriptions", {
  method: "POST", headers: { "Content-Type": "application/json" },
  body: JSON.stringify({ plan_id: "pro", interval: "monthly", metadata: { user_id: "42" } }),
})).json();
window.location = url;
```

---

### Subscription management

| Endpoint | Payload | Notes |
|---|---|---|
| `POST /api/subscriptions/cancel` | `{ "subscription_id": "sub_...", "cancel_at_period_end": true }` | `false` cancels immediately |
| `POST /api/subscriptions/pause` | `{ "subscription_id": "sub_..." }` | pauses collection (voids invoices while paused) |
| `POST /api/subscriptions/resume` | `{ "subscription_id": "sub_..." }` | clears the pause |
| `POST /api/subscriptions/change-plan` | `{ "subscription_id": "sub_...", "new_plan_id": "pro", "interval": "yearly", "prorate": true }` | upgrade/downgrade |
| `POST /api/subscriptions/portal` | `{ "customer_id": "cus_...", "return_url": "https://app/account" }` | Stripe-hosted self-service page; redirect to the returned `url` |
| `GET /api/subscriptions/{id}` | — (id in URL) | fetch current status |

All subscription endpoints (except `portal`) return the normalized
`Subscription` object:
```json
{
  "id": "sub_...",
  "status": "active",
  "customer_id": "cus_...",
  "price_id": "price_...",
  "current_period_end": 1718000000,
  "cancel_at_period_end": false,
  "paused": false
}
```
`change-plan` and `cancel` also fill `message` and `effective_at`.

The **Billing Portal** is the easiest way to let customers self-manage — you only
need their Stripe `customer_id` (you get it from the
`customer.subscription.created` webhook):
```js
const { url } = await (await fetch("/api/subscriptions/portal", {
  method: "POST", headers: { "Content-Type": "application/json" },
  body: JSON.stringify({ customer_id: "cus_123", return_url: "https://app/account" }),
})).json();
window.location = url;
```

---

### `POST /api/webhooks/stripe` — webhook receiver

**Not called by your frontend.** Stripe calls this server-to-server. This is
where real fulfilment happens (idempotently). Configure the URL and copy the
signing secret into `STRIPE_WEBHOOK_SECRET` (Dashboard → Developers → Webhooks).

---

## Notes & gotchas

- **Amounts in requests are in the major unit (e.g. dollars).** The backend
  converts to the smallest unit (cents). Zero-decimal currencies (JPY, KRW, …)
  are passed through unchanged.
- **The webhook is the source of truth.** Don't grant access on the success page
  — treat it as "show the thank-you screen". Grant entitlements from the webhook.
- **`metadata` is your friend.** Anything you put there comes back in the
  webhook, so stash your user/order ids there to reconcile.
- **CORS:** the example server allows all origins (`*`) for dev. Lock this down
  in production (`cmd/server/main.go` → `withCORS`).

## Server env vars (so the backend actually works)

See `.env.example`. Minimum to set:
```
STRIPE_SECRET_KEY=sk_test_xxx
STRIPE_PUBLISHABLE_KEY=pk_test_xxx
STRIPE_WEBHOOK_SECRET=whsec_xxx          # needed if you mount the webhook
STRIPE_CURRENCY=usd
FRONTEND_URL=http://localhost:3000       # Portal return + Checkout redirect base
# subscription price mapping (internal:interval -> Stripe Price id):
STRIPE_PRO_MONTHLY_PRICE_ID=price_xxx
STRIPE_PRO_QUARTERLY_PRICE_ID=price_xxx
STRIPE_PRO_YEARLY_PRICE_ID=price_xxx
```
