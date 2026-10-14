package stripe

import (
	"errors"

	stripe "github.com/stripe/stripe-go/v81"
	"github.com/stripe/stripe-go/v81/client"
)

var (
	// ErrPriceNotResolved is returned by a Hooks.ResolvePrice implementation
	// (or the NoopHooks default) when an internal plan id cannot be mapped to a
	// Stripe Price id.
	ErrPriceNotResolved = errors.New("stripe: price could not be resolved")
	// ErrInvalidSignature is returned when a webhook signature check fails.
	ErrInvalidSignature = errors.New("stripe: invalid signature")
	// ErrNoSubscriptionItem is returned by ChangePlan when the subscription has
	// no line items to swap.
	ErrNoSubscriptionItem = errors.New("stripe: subscription has no items to change")
)

// Client is the reusable Stripe wrapper. It owns a Stripe API client and your
// Hooks implementation, and exposes high-level operations (Checkout, Payment
// Intents, subscriptions, webhooks). Construct it once and share it.
type Client struct {
	cfg   Config
	api   *client.API
	hooks Hooks
}

func New(cfg Config, hooks Hooks) *Client {
	if hooks == nil {
		hooks = NoopHooks{}
	}
	api := &client.API{}
	api.Init(cfg.SecretKey, nil)
	return &Client{
		cfg:   cfg,
		api:   api,
		hooks: hooks,
	}
}

// PublishableKey is the only Stripe key the browser ever needs.
func (c *Client) PublishableKey() string { return c.cfg.PublishableKey }

// stripeString is a tiny local helper so callers don't import the SDK just to
// build pointers; it mirrors stripe.String.
func stripeString(s string) *string { return stripe.String(s) }
