package stripe

import "context"

// Hooks is THE integration point. The stripe package handles everything
// Stripe-specific (Checkout, Payment Intents, subscriptions, signature
// verification, webhook parsing) and calls back into your code at the moments
// where your application needs to do something: resolve a price, persist a
// pending checkout, fulfil a payment, credit a billing period.
//
// Embed NoopHooks to inherit no-op defaults and override only what you need.
//
// IMPORTANT: the On* methods that fulfil value (OnPaymentSucceeded, OnInvoicePaid,
// the subscription lifecycle hooks) are driven by WEBHOOKS, which Stripe retries
// until you return success. Make them IDEMPOTENT — key off the Stripe object id
// (PaymentIntentID, InvoiceID, SubscriptionID) so a retry is a no-op.
type Hooks interface {
	// ResolvePrice maps your internal plan id + interval to a Stripe Price id
	// (price_...). Return ErrPriceNotResolved if it can't be mapped.
	ResolvePrice(ctx context.Context, planID string, interval Interval) (stripePriceID string, err error)

	// OnCheckoutCreated fires when a Checkout Session is created (one-time or
	// subscription). Persist the pending session so you can reconcile later.
	OnCheckoutCreated(ctx context.Context, e CheckoutCreated) error
	// OnSubscriptionCreated fires when a subscription Checkout Session is created.
	OnSubscriptionCreated(ctx context.Context, e SubscriptionCreated) error

	// OnPaymentSucceeded fires from the webhook when a one-time payment is
	// captured (checkout.session.completed in payment mode, or
	// payment_intent.succeeded). FULFIL HERE. Idempotent.
	OnPaymentSucceeded(ctx context.Context, e PaymentSucceeded) error
	// OnPaymentFailed fires when a payment or invoice charge fails.
	OnPaymentFailed(ctx context.Context, e PaymentFailed) error

	// OnSubscriptionActivated fires when a subscription becomes active/trialing.
	OnSubscriptionActivated(ctx context.Context, e SubscriptionEvent) error
	// OnInvoicePaid fires on every successful recurring charge (invoice.paid).
	// Credit the new billing period here. Idempotent on InvoiceID.
	OnInvoicePaid(ctx context.Context, e InvoicePaid) error
	// OnSubscriptionUpdated fires for subscription.updated events that are not
	// specifically a pause/resume (e.g. plan change took effect).
	OnSubscriptionUpdated(ctx context.Context, e SubscriptionEvent) error
	// OnSubscriptionPaused fires when collection is paused.
	OnSubscriptionPaused(ctx context.Context, e SubscriptionEvent) error
	// OnSubscriptionResumed fires when a paused subscription resumes collection.
	OnSubscriptionResumed(ctx context.Context, e SubscriptionEvent) error
	// OnSubscriptionCancelled fires when a subscription is deleted/cancelled.
	OnSubscriptionCancelled(ctx context.Context, e SubscriptionEvent) error
}

// CheckoutCreated is emitted when a Checkout Session is created.
type CheckoutCreated struct {
	SessionID string
	Mode      string // "payment" or "subscription"
	Amount    int64  // major unit, as requested (0 for subscriptions)
	Currency  string
	Metadata  map[string]string
}

// SubscriptionCreated is emitted when a subscription Checkout Session is created.
type SubscriptionCreated struct {
	SessionID     string
	StripePriceID string
	Interval      Interval
	Metadata      map[string]string
}

// PaymentSucceeded is emitted from the webhook on a successful one-time payment.
type PaymentSucceeded struct {
	Event           string
	SessionID       string
	PaymentIntentID string
	CustomerID      string
	CustomerEmail   string
	Amount          int64 // smallest unit, as Stripe reports it
	Currency        string
	Metadata        map[string]string
}

// PaymentFailed is emitted when a payment or invoice charge fails.
type PaymentFailed struct {
	Event            string
	PaymentIntentID  string
	InvoiceID        string
	SubscriptionID   string
	ErrorCode        string
	ErrorDescription string
	Metadata         map[string]string
}

// SubscriptionEvent is emitted for subscription lifecycle webhooks.
type SubscriptionEvent struct {
	Event             string
	SubscriptionID    string
	CustomerID        string
	StripePriceID     string
	Status            string
	CancelAtPeriodEnd bool
	Interval          Interval
	Metadata          map[string]string
}

// InvoicePaid is emitted on every successful recurring subscription charge.
type InvoicePaid struct {
	Event           string
	InvoiceID       string
	SubscriptionID  string
	CustomerID      string
	PaymentIntentID string
	StripePriceID   string
	Amount          int64
	Currency        string
	Interval        Interval
	Metadata        map[string]string
}

// NoopHooks provides no-op defaults for every hook except ResolvePrice (which
// must fail loudly so an unmapped plan doesn't silently create a bad checkout).
// Embed it in your own struct and override what you need.
type NoopHooks struct{}

func (NoopHooks) ResolvePrice(context.Context, string, Interval) (string, error) {
	return "", ErrPriceNotResolved
}
func (NoopHooks) OnCheckoutCreated(context.Context, CheckoutCreated) error         { return nil }
func (NoopHooks) OnSubscriptionCreated(context.Context, SubscriptionCreated) error { return nil }
func (NoopHooks) OnPaymentSucceeded(context.Context, PaymentSucceeded) error       { return nil }
func (NoopHooks) OnPaymentFailed(context.Context, PaymentFailed) error             { return nil }
func (NoopHooks) OnSubscriptionActivated(context.Context, SubscriptionEvent) error { return nil }
func (NoopHooks) OnInvoicePaid(context.Context, InvoicePaid) error                 { return nil }
func (NoopHooks) OnSubscriptionUpdated(context.Context, SubscriptionEvent) error   { return nil }
func (NoopHooks) OnSubscriptionPaused(context.Context, SubscriptionEvent) error    { return nil }
func (NoopHooks) OnSubscriptionResumed(context.Context, SubscriptionEvent) error   { return nil }
func (NoopHooks) OnSubscriptionCancelled(context.Context, SubscriptionEvent) error { return nil }
