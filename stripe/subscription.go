package stripe

import (
	"context"
	"fmt"
	"time"

	stripe "github.com/stripe/stripe-go/v81"
)

// CreateSubscription creates a Stripe-hosted Checkout Session in subscription
// mode. The caller redirects the browser to the returned URL; the customer
// enters their card and the mandate is set up by Stripe. Fulfilment happens in
// the webhooks (customer.subscription.created, invoice.paid).
func (c *Client) CreateSubscription(ctx context.Context, req SubscriptionRequest) (*CheckoutSession, error) {
	interval := req.Interval.Normalize()

	priceID, err := c.hooks.ResolvePrice(ctx, req.PlanID, interval)
	if err != nil {
		return nil, err
	}
	if priceID == "" {
		return nil, ErrPriceNotResolved
	}

	// Stash the interval in metadata so webhooks can recover it later.
	md := map[string]string{}
	for k, v := range req.Metadata {
		md[k] = v
	}
	md["interval"] = string(interval)

	params := &stripe.CheckoutSessionParams{
		Mode:       stripeString(string(stripe.CheckoutSessionModeSubscription)),
		SuccessURL: stripeString(c.successURL(req.SuccessURL)),
		CancelURL:  stripeString(c.cancelURL(req.CancelURL)),
		LineItems: []*stripe.CheckoutSessionLineItemParams{
			{
				Price:    stripeString(priceID),
				Quantity: stripe.Int64(1),
			},
		},
		SubscriptionData: &stripe.CheckoutSessionSubscriptionDataParams{
			Metadata: md,
		},
	}
	if req.CustomerEmail != "" {
		params.CustomerEmail = stripeString(req.CustomerEmail)
	}
	if req.TrialDays > 0 {
		params.SubscriptionData.TrialPeriodDays = stripe.Int64(req.TrialDays)
	}
	// Metadata on the session (subscription metadata set above).
	applyMetadata(&params.Params, md)
	params.Context = ctx

	s, err := c.api.CheckoutSessions.New(params)
	if err != nil {
		return nil, fmt.Errorf("stripe: create subscription checkout: %w", err)
	}

	if err := c.hooks.OnSubscriptionCreated(ctx, SubscriptionCreated{
		SessionID:     s.ID,
		StripePriceID: priceID,
		Interval:      interval,
		Metadata:      req.Metadata,
	}); err != nil {
		return nil, fmt.Errorf("stripe: OnSubscriptionCreated hook: %w", err)
	}

	return &CheckoutSession{
		ID:            s.ID,
		URL:           s.URL,
		Mode:          string(s.Mode),
		Status:        string(s.Status),
		PaymentStatus: string(s.PaymentStatus),
		Currency:      string(s.Currency),
	}, nil
}

// FetchSubscription returns the live status of a subscription.
func (c *Client) FetchSubscription(subscriptionID string) (*Subscription, error) {
	sub, err := c.api.Subscriptions.Get(subscriptionID, nil)
	if err != nil {
		return nil, fmt.Errorf("stripe: fetch subscription: %w", err)
	}
	return normalizeSubscription(sub), nil
}

// CancelSubscription cancels a subscription. When cancelAtPeriodEnd is true the
// subscription stays active until the end of the paid period; otherwise it is
// cancelled immediately.
func (c *Client) CancelSubscription(subscriptionID string, cancelAtPeriodEnd bool) (*Subscription, error) {
	if cancelAtPeriodEnd {
		sub, err := c.api.Subscriptions.Update(subscriptionID, &stripe.SubscriptionParams{
			CancelAtPeriodEnd: stripe.Bool(true),
		})
		if err != nil {
			return nil, fmt.Errorf("stripe: schedule cancel: %w", err)
		}
		out := normalizeSubscription(sub)
		out.Message = "Subscription will cancel at the end of the current billing period."
		out.EffectiveAt = out.CurrentPeriodEnd
		return out, nil
	}

	sub, err := c.api.Subscriptions.Cancel(subscriptionID, nil)
	if err != nil {
		return nil, fmt.Errorf("stripe: cancel subscription: %w", err)
	}
	out := normalizeSubscription(sub)
	out.Message = "Subscription cancelled immediately."
	return out, nil
}

// PauseSubscription pauses payment collection (Stripe keeps the subscription but
// stops charging). Invoices created while paused are voided.
func (c *Client) PauseSubscription(subscriptionID string) (*Subscription, error) {
	sub, err := c.api.Subscriptions.Update(subscriptionID, &stripe.SubscriptionParams{
		PauseCollection: &stripe.SubscriptionPauseCollectionParams{
			Behavior: stripeString("void"),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("stripe: pause subscription: %w", err)
	}
	return normalizeSubscription(sub), nil
}

// ResumeSubscription clears a pause_collection, resuming normal billing.
func (c *Client) ResumeSubscription(subscriptionID string) (*Subscription, error) {
	params := &stripe.SubscriptionParams{}
	// Clearing pause_collection requires sending an explicit empty value.
	params.AddExtra("pause_collection", "")
	sub, err := c.api.Subscriptions.Update(subscriptionID, params)
	if err != nil {
		return nil, fmt.Errorf("stripe: resume subscription: %w", err)
	}
	return normalizeSubscription(sub), nil
}

// ChangePlan swaps the subscription's price to a new plan, prorating the change.
// newPlanID is YOUR internal plan id; it is resolved to a Stripe Price via the
// hook. The change takes effect immediately with prorations by default.
func (c *Client) ChangePlan(ctx context.Context, subscriptionID, newPlanID string, interval Interval, prorate bool) (*Subscription, error) {
	priceID, err := c.hooks.ResolvePrice(ctx, newPlanID, interval.Normalize())
	if err != nil {
		return nil, err
	}
	if priceID == "" {
		return nil, ErrPriceNotResolved
	}

	current, err := c.api.Subscriptions.Get(subscriptionID, nil)
	if err != nil {
		return nil, fmt.Errorf("stripe: fetch subscription for change: %w", err)
	}
	if current.Items == nil || len(current.Items.Data) == 0 {
		return nil, ErrNoSubscriptionItem
	}
	itemID := current.Items.Data[0].ID

	prorationBehavior := "create_prorations"
	if !prorate {
		prorationBehavior = "none"
	}

	params := &stripe.SubscriptionParams{
		ProrationBehavior: stripeString(prorationBehavior),
		Items: []*stripe.SubscriptionItemsParams{
			{
				ID:    stripeString(itemID),
				Price: stripeString(priceID),
			},
		},
	}
	params.AddMetadata("interval", string(interval.Normalize()))
	params.Context = ctx

	sub, err := c.api.Subscriptions.Update(subscriptionID, params)
	if err != nil {
		return nil, fmt.Errorf("stripe: change plan: %w", err)
	}
	out := normalizeSubscription(sub)
	out.EffectiveAt = time.Now().Unix()
	out.Message = "Plan changed."
	return out, nil
}

// CreatePortalSession opens a Stripe Billing Portal session for self-service
// subscription management (update card, cancel, view invoices).
func (c *Client) CreatePortalSession(ctx context.Context, req PortalRequest) (*PortalSession, error) {
	returnURL := req.ReturnURL
	if returnURL == "" {
		returnURL = c.cfg.FrontendURL
	}
	params := &stripe.BillingPortalSessionParams{
		Customer:  stripeString(req.CustomerID),
		ReturnURL: stripeString(returnURL),
	}
	params.Context = ctx

	ps, err := c.api.BillingPortalSessions.New(params)
	if err != nil {
		return nil, fmt.Errorf("stripe: create billing portal session: %w", err)
	}
	return &PortalSession{URL: ps.URL}, nil
}

// normalizeSubscription flattens a Stripe subscription into our compact shape.
func normalizeSubscription(sub *stripe.Subscription) *Subscription {
	out := &Subscription{
		ID:                sub.ID,
		Status:            string(sub.Status),
		CurrentPeriodEnd:  sub.CurrentPeriodEnd,
		CancelAtPeriodEnd: sub.CancelAtPeriodEnd,
		CanceledAt:        sub.CanceledAt,
		Paused:            sub.PauseCollection != nil,
	}
	if sub.Customer != nil {
		out.CustomerID = sub.Customer.ID
	}
	if sub.Items != nil && len(sub.Items.Data) > 0 && sub.Items.Data[0].Price != nil {
		out.PriceID = sub.Items.Data[0].Price.ID
	}
	return out
}
