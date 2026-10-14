package main

import (
	"context"
	"log"
	"os"

	"github.com/example/stripe-template/stripe"
)

// ExampleHooks is a runnable, in-memory illustration of the only thing you must
// write. In a real app these methods would touch your database and grant value.
// Embed stripe.NoopHooks so you only override what you care about.
type ExampleHooks struct {
	stripe.NoopHooks
}

// priceMap maps "<planID>:<interval>" to a Stripe Price id, sourced from env.
var priceMap = map[string]string{
	"pro:monthly":   os.Getenv("STRIPE_PRO_MONTHLY_PRICE_ID"),
	"pro:quarterly": os.Getenv("STRIPE_PRO_QUARTERLY_PRICE_ID"),
	"pro:yearly":    os.Getenv("STRIPE_PRO_YEARLY_PRICE_ID"),
}

func (ExampleHooks) ResolvePrice(_ context.Context, planID string, interval stripe.Interval) (string, error) {
	if priceID, ok := priceMap[planID+":"+string(interval)]; ok && priceID != "" {
		return priceID, nil
	}
	return "", stripe.ErrPriceNotResolved
}

func (ExampleHooks) OnCheckoutCreated(_ context.Context, e stripe.CheckoutCreated) error {
	log.Printf("[hook] checkout created: id=%s mode=%s amount=%d %s meta=%v",
		e.SessionID, e.Mode, e.Amount, e.Currency, e.Metadata)
	return nil
}

func (ExampleHooks) OnSubscriptionCreated(_ context.Context, e stripe.SubscriptionCreated) error {
	log.Printf("[hook] subscription checkout created: id=%s price=%s interval=%s meta=%v",
		e.SessionID, e.StripePriceID, e.Interval, e.Metadata)
	return nil
}

func (ExampleHooks) OnPaymentSucceeded(_ context.Context, e stripe.PaymentSucceeded) error {
	log.Printf("[hook] PAYMENT SUCCEEDED: session=%s pi=%s amount=%d %s email=%s meta=%v -- fulfil here (idempotent)",
		e.SessionID, e.PaymentIntentID, e.Amount, e.Currency, e.CustomerEmail, e.Metadata)
	return nil
}

func (ExampleHooks) OnPaymentFailed(_ context.Context, e stripe.PaymentFailed) error {
	log.Printf("[hook] payment failed: pi=%s invoice=%s sub=%s err=%s (%s)",
		e.PaymentIntentID, e.InvoiceID, e.SubscriptionID, e.ErrorDescription, e.ErrorCode)
	return nil
}

func (ExampleHooks) OnSubscriptionActivated(_ context.Context, e stripe.SubscriptionEvent) error {
	log.Printf("[hook] subscription activated: id=%s status=%s price=%s meta=%v",
		e.SubscriptionID, e.Status, e.StripePriceID, e.Metadata)
	return nil
}

func (ExampleHooks) OnInvoicePaid(_ context.Context, e stripe.InvoicePaid) error {
	log.Printf("[hook] INVOICE PAID: invoice=%s sub=%s amount=%d %s interval=%s -- credit the period here (idempotent on invoice id)",
		e.InvoiceID, e.SubscriptionID, e.Amount, e.Currency, e.Interval)
	return nil
}

func (ExampleHooks) OnSubscriptionUpdated(_ context.Context, e stripe.SubscriptionEvent) error {
	log.Printf("[hook] subscription updated: id=%s status=%s cancel_at_period_end=%v price=%s",
		e.SubscriptionID, e.Status, e.CancelAtPeriodEnd, e.StripePriceID)
	return nil
}

func (ExampleHooks) OnSubscriptionPaused(_ context.Context, e stripe.SubscriptionEvent) error {
	log.Printf("[hook] subscription paused: id=%s", e.SubscriptionID)
	return nil
}

func (ExampleHooks) OnSubscriptionResumed(_ context.Context, e stripe.SubscriptionEvent) error {
	log.Printf("[hook] subscription resumed: id=%s", e.SubscriptionID)
	return nil
}

func (ExampleHooks) OnSubscriptionCancelled(_ context.Context, e stripe.SubscriptionEvent) error {
	log.Printf("[hook] subscription cancelled: id=%s", e.SubscriptionID)
	return nil
}
