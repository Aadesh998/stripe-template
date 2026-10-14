package stripe

import (
	"context"
	"fmt"

	stripe "github.com/stripe/stripe-go/v81"
)

// CreateCheckoutSession creates a one-time, Stripe-hosted Checkout page. The
// caller redirects the browser to the returned URL. Fulfilment happens later in
// the webhook (checkout.session.completed), not here.
func (c *Client) CreateCheckoutSession(ctx context.Context, req CheckoutRequest) (*CheckoutSession, error) {
	currency := req.Currency
	if currency == "" {
		currency = c.cfg.Currency
	}
	quantity := req.Quantity
	if quantity <= 0 {
		quantity = 1
	}
	productName := req.ProductName
	if productName == "" {
		productName = "Payment"
	}

	params := &stripe.CheckoutSessionParams{
		Mode:       stripeString(string(stripe.CheckoutSessionModePayment)),
		SuccessURL: stripeString(c.successURL(req.SuccessURL)),
		CancelURL:  stripeString(c.cancelURL(req.CancelURL)),
		LineItems: []*stripe.CheckoutSessionLineItemParams{
			{
				Quantity: stripe.Int64(quantity),
				PriceData: &stripe.CheckoutSessionLineItemPriceDataParams{
					Currency:   stripeString(currency),
					UnitAmount: stripe.Int64(toSmallestUnit(req.Amount, currency)),
					ProductData: &stripe.CheckoutSessionLineItemPriceDataProductDataParams{
						Name:        stripeString(productName),
						Description: optionalString(req.Description),
					},
				},
			},
		},
	}
	if req.CustomerEmail != "" {
		params.CustomerEmail = stripeString(req.CustomerEmail)
	}
	// Propagate metadata both to the session and to the resulting PaymentIntent
	// so it is available no matter which webhook you key off.
	applyMetadata(&params.Params, req.Metadata)
	params.PaymentIntentData = &stripe.CheckoutSessionPaymentIntentDataParams{
		Metadata: req.Metadata,
	}
	params.Context = ctx

	s, err := c.api.CheckoutSessions.New(params)
	if err != nil {
		return nil, fmt.Errorf("stripe: create checkout session: %w", err)
	}

	if err := c.hooks.OnCheckoutCreated(ctx, CheckoutCreated{
		SessionID: s.ID,
		Mode:      string(s.Mode),
		Amount:    req.Amount,
		Currency:  currency,
		Metadata:  req.Metadata,
	}); err != nil {
		return nil, fmt.Errorf("stripe: OnCheckoutCreated hook: %w", err)
	}

	return &CheckoutSession{
		ID:            s.ID,
		URL:           s.URL,
		Mode:          string(s.Mode),
		Status:        string(s.Status),
		PaymentStatus: string(s.PaymentStatus),
		AmountTotal:   s.AmountTotal,
		Currency:      string(s.Currency),
	}, nil
}

// CreatePaymentIntent creates a PaymentIntent for a custom in-page checkout
// built with Stripe Elements. The browser confirms the returned client_secret.
// Fulfilment happens in the webhook (payment_intent.succeeded).
func (c *Client) CreatePaymentIntent(ctx context.Context, req PaymentIntentRequest) (*PaymentIntentResult, error) {
	currency := req.Currency
	if currency == "" {
		currency = c.cfg.Currency
	}

	params := &stripe.PaymentIntentParams{
		Amount:   stripe.Int64(toSmallestUnit(req.Amount, currency)),
		Currency: stripeString(currency),
		AutomaticPaymentMethods: &stripe.PaymentIntentAutomaticPaymentMethodsParams{
			Enabled: stripe.Bool(true),
		},
	}
	if req.Description != "" {
		params.Description = stripeString(req.Description)
	}
	if req.CustomerEmail != "" {
		params.ReceiptEmail = stripeString(req.CustomerEmail)
	}
	applyMetadata(&params.Params, req.Metadata)
	params.Context = ctx

	pi, err := c.api.PaymentIntents.New(params)
	if err != nil {
		return nil, fmt.Errorf("stripe: create payment intent: %w", err)
	}

	if err := c.hooks.OnCheckoutCreated(ctx, CheckoutCreated{
		SessionID: pi.ID,
		Mode:      "payment_intent",
		Amount:    req.Amount,
		Currency:  currency,
		Metadata:  req.Metadata,
	}); err != nil {
		return nil, fmt.Errorf("stripe: OnCheckoutCreated hook: %w", err)
	}

	return &PaymentIntentResult{
		ID:           pi.ID,
		ClientSecret: pi.ClientSecret,
		Status:       string(pi.Status),
		Amount:       pi.Amount,
		Currency:     string(pi.Currency),
	}, nil
}

func (c *Client) successURL(override string) string {
	if override != "" {
		return override
	}
	if c.cfg.SuccessURL != "" {
		return c.cfg.SuccessURL
	}
	return "https://example.com/success?session_id={CHECKOUT_SESSION_ID}"
}

func (c *Client) cancelURL(override string) string {
	if override != "" {
		return override
	}
	if c.cfg.CancelURL != "" {
		return c.cfg.CancelURL
	}
	return "https://example.com/cancel"
}

func optionalString(s string) *string {
	if s == "" {
		return nil
	}
	return stripeString(s)
}

// applyMetadata copies a string map onto any stripe.Params via AddMetadata,
// which is the SDK-blessed way to set metadata keys.
func applyMetadata(p *stripe.Params, md map[string]string) {
	for k, v := range md {
		p.AddMetadata(k, v)
	}
}
