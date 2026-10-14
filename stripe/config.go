package stripe

import (
	"fmt"
	"os"
)

// Config holds everything the Stripe integration needs. Only SecretKey is
// strictly required to talk to the API; WebhookSecret is required if you mount
// the webhook receiver; the URLs are required for Checkout (Stripe redirects the
// browser to them after the hosted page).
type Config struct {
	SecretKey      string // sk_test_... / sk_live_... (server-side only)
	PublishableKey string // pk_test_... / pk_live_... (safe to expose to the browser)
	WebhookSecret  string // whsec_... (Dashboard -> Developers -> Webhooks)
	Currency       string // default currency for ad-hoc Checkout / PaymentIntents

	// Where Stripe sends the browser after a hosted Checkout. If the per-request
	// values are empty these defaults are used. SuccessURL may contain the
	// literal "{CHECKOUT_SESSION_ID}" placeholder, which Stripe expands.
	SuccessURL string
	CancelURL  string

	// FrontendURL is used as the return target for the Billing Portal and as a
	// fallback to derive Success/Cancel URLs when those are not set explicitly.
	FrontendURL string
}

func LoadConfigFromEnv() (Config, error) {
	cfg := Config{
		SecretKey:      os.Getenv("STRIPE_SECRET_KEY"),
		PublishableKey: os.Getenv("STRIPE_PUBLISHABLE_KEY"),
		WebhookSecret:  os.Getenv("STRIPE_WEBHOOK_SECRET"),
		Currency:       os.Getenv("STRIPE_CURRENCY"),
		SuccessURL:     os.Getenv("STRIPE_SUCCESS_URL"),
		CancelURL:      os.Getenv("STRIPE_CANCEL_URL"),
		FrontendURL:    os.Getenv("FRONTEND_URL"),
	}

	if cfg.Currency == "" {
		cfg.Currency = "usd"
	}

	// Derive sensible Checkout redirect URLs from FRONTEND_URL when not given.
	if cfg.FrontendURL != "" {
		if cfg.SuccessURL == "" {
			cfg.SuccessURL = cfg.FrontendURL + "/success?session_id={CHECKOUT_SESSION_ID}"
		}
		if cfg.CancelURL == "" {
			cfg.CancelURL = cfg.FrontendURL + "/cancel"
		}
	}

	if cfg.SecretKey == "" {
		return Config{}, fmt.Errorf("stripe: STRIPE_SECRET_KEY is required")
	}
	return cfg, nil
}
