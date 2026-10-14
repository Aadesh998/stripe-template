package stripe

// Interval is a human label for a subscription's billing cadence. Stripe itself
// encodes cadence on the Price object, so this value is only used to pick the
// right Price via Hooks.ResolvePrice and is echoed into metadata so webhooks can
// recover it.
type Interval string

const (
	IntervalMonthly   Interval = "monthly"
	IntervalQuarterly Interval = "quarterly"
	IntervalYearly    Interval = "yearly"
)

func (i Interval) Normalize() Interval {
	switch i {
	case IntervalQuarterly:
		return IntervalQuarterly
	case IntervalYearly:
		return IntervalYearly
	default:
		return IntervalMonthly
	}
}

// CheckoutRequest creates a one-time, Stripe-hosted Checkout page. Amount is in
// the MAJOR unit (e.g. dollars); the backend converts to the smallest unit.
type CheckoutRequest struct {
	Amount        int64             `json:"amount"`
	Currency      string            `json:"currency"`
	ProductName   string            `json:"product_name"`
	Description   string            `json:"description"`
	Quantity      int64             `json:"quantity"`
	CustomerEmail string            `json:"customer_email"`
	SuccessURL    string            `json:"success_url"`
	CancelURL     string            `json:"cancel_url"`
	Metadata      map[string]string `json:"metadata"`
}

// CheckoutSession is the normalized result of creating a Checkout Session for
// either a one-time payment or a subscription.
type CheckoutSession struct {
	ID            string `json:"id"`
	URL           string `json:"url"` // redirect the browser here
	Mode          string `json:"mode"`
	Status        string `json:"status"`
	PaymentStatus string `json:"payment_status"`
	AmountTotal   int64  `json:"amount_total"`
	Currency      string `json:"currency"`
}

// PaymentIntentRequest creates a PaymentIntent for a custom in-page checkout
// built with Stripe Elements (you confirm the client_secret in the browser).
type PaymentIntentRequest struct {
	Amount        int64             `json:"amount"`
	Currency      string            `json:"currency"`
	Description   string            `json:"description"`
	CustomerEmail string            `json:"customer_email"`
	Metadata      map[string]string `json:"metadata"`
}

// PaymentIntentResult carries the client_secret the browser needs to confirm.
type PaymentIntentResult struct {
	ID           string `json:"id"`
	ClientSecret string `json:"client_secret"`
	Status       string `json:"status"`
	Amount       int64  `json:"amount"`
	Currency     string `json:"currency"`
}

// SubscriptionRequest creates a subscription Checkout Session. PlanID is YOUR
// internal id; the backend maps PlanID+Interval to a Stripe Price via the hook.
type SubscriptionRequest struct {
	PlanID        string            `json:"plan_id"`
	Interval      Interval          `json:"interval"`
	CustomerEmail string            `json:"customer_email"`
	TrialDays     int64             `json:"trial_days"`
	SuccessURL    string            `json:"success_url"`
	CancelURL     string            `json:"cancel_url"`
	Metadata      map[string]string `json:"metadata"`
}

// Subscription is the normalized shape of a Stripe subscription returned by the
// management endpoints.
type Subscription struct {
	ID                 string `json:"id"`
	Status             string `json:"status"`
	CustomerID         string `json:"customer_id"`
	PriceID            string `json:"price_id"`
	CurrentPeriodEnd   int64  `json:"current_period_end"`
	CancelAtPeriodEnd  bool   `json:"cancel_at_period_end"`
	CanceledAt         int64  `json:"canceled_at"`
	Paused             bool   `json:"paused"`
	Message            string `json:"message,omitempty"`
	EffectiveAt        int64  `json:"effective_at,omitempty"`
}

// PortalRequest opens a Stripe Billing Portal session so a customer can manage
// their own subscription, payment methods and invoices on a Stripe-hosted page.
type PortalRequest struct {
	CustomerID string `json:"customer_id"`
	ReturnURL  string `json:"return_url"`
}

// PortalSession is the result of creating a Billing Portal session.
type PortalSession struct {
	URL string `json:"url"`
}
