package stripe

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/stripe/stripe-go/v81/webhook"
)

// VerifyWebhookSignature checks the Stripe-Signature header against the raw body
// using the configured signing secret (includes Stripe's timestamp tolerance).
func (c *Client) VerifyWebhookSignature(body []byte, signature string) error {
	if c.cfg.WebhookSecret == "" {
		return fmt.Errorf("stripe: webhook secret not configured")
	}
	if _, err := webhook.ConstructEvent(body, signature, c.cfg.WebhookSecret); err != nil {
		return ErrInvalidSignature
	}
	return nil
}

// HandleWebhook verifies the signature, parses the event and dispatches to the
// matching hook. Return value: a non-nil error makes the transport answer 5xx so
// Stripe retries — so your hooks must be idempotent.
func (c *Client) HandleWebhook(ctx context.Context, body []byte, signature string) error {
	if c.cfg.WebhookSecret == "" {
		return fmt.Errorf("stripe: webhook secret not configured")
	}
	event, err := webhook.ConstructEvent(body, signature, c.cfg.WebhookSecret)
	if err != nil {
		return ErrInvalidSignature
	}

	switch string(event.Type) {

	case "checkout.session.completed", "checkout.session.async_payment_succeeded":
		var s checkoutSessionEntity
		if err := json.Unmarshal(event.Data.Raw, &s); err != nil {
			return fmt.Errorf("stripe: parse checkout session: %w", err)
		}
		// Subscription-mode sessions are fulfilled via the subscription/invoice
		// webhooks; only fulfil one-time payments here.
		if s.Mode == "subscription" {
			return nil
		}
		if s.PaymentStatus != "" && s.PaymentStatus != "paid" {
			return nil
		}
		return c.hooks.OnPaymentSucceeded(ctx, PaymentSucceeded{
			Event:           string(event.Type),
			SessionID:       s.ID,
			PaymentIntentID: s.PaymentIntent,
			CustomerID:      s.Customer,
			CustomerEmail:   s.email(),
			Amount:          s.AmountTotal,
			Currency:        s.Currency,
			Metadata:        s.Metadata,
		})

	case "payment_intent.succeeded":
		var pi paymentIntentEntity
		if err := json.Unmarshal(event.Data.Raw, &pi); err != nil {
			return fmt.Errorf("stripe: parse payment intent: %w", err)
		}
		// If this PaymentIntent belongs to an invoice, the invoice.paid webhook
		// is the right place to fulfil — skip to avoid double fulfilment.
		if pi.Invoice != "" {
			return nil
		}
		return c.hooks.OnPaymentSucceeded(ctx, PaymentSucceeded{
			Event:           string(event.Type),
			PaymentIntentID: pi.ID,
			CustomerID:      pi.Customer,
			CustomerEmail:   pi.ReceiptEmail,
			Amount:          pi.Amount,
			Currency:        pi.Currency,
			Metadata:        pi.Metadata,
		})

	case "payment_intent.payment_failed":
		var pi paymentIntentEntity
		if err := json.Unmarshal(event.Data.Raw, &pi); err != nil {
			return fmt.Errorf("stripe: parse payment intent: %w", err)
		}
		e := PaymentFailed{
			Event:           string(event.Type),
			PaymentIntentID: pi.ID,
			Metadata:        pi.Metadata,
		}
		if pi.LastPaymentError != nil {
			e.ErrorCode = pi.LastPaymentError.Code
			e.ErrorDescription = pi.LastPaymentError.Message
		}
		return c.hooks.OnPaymentFailed(ctx, e)

	case "invoice.paid", "invoice.payment_succeeded":
		var inv invoiceEntity
		if err := json.Unmarshal(event.Data.Raw, &inv); err != nil {
			return fmt.Errorf("stripe: parse invoice: %w", err)
		}
		subID := inv.subscriptionID()
		// invoice.paid also fires for non-subscription invoices; only the ones
		// tied to a subscription are recurring charges we care about here.
		if subID == "" {
			return nil
		}
		return c.hooks.OnInvoicePaid(ctx, InvoicePaid{
			Event:           string(event.Type),
			InvoiceID:       inv.ID,
			SubscriptionID:  subID,
			CustomerID:      inv.Customer,
			PaymentIntentID: inv.PaymentIntent,
			StripePriceID:   inv.priceID(),
			Amount:          inv.AmountPaid,
			Currency:        inv.Currency,
			Interval:        intervalFromMetadata(inv.Metadata),
			Metadata:        inv.Metadata,
		})

	case "invoice.payment_failed":
		var inv invoiceEntity
		if err := json.Unmarshal(event.Data.Raw, &inv); err != nil {
			return fmt.Errorf("stripe: parse invoice: %w", err)
		}
		return c.hooks.OnPaymentFailed(ctx, PaymentFailed{
			Event:          string(event.Type),
			InvoiceID:      inv.ID,
			SubscriptionID: inv.subscriptionID(),
			Metadata:       inv.Metadata,
		})

	case "customer.subscription.created":
		return c.hooks.OnSubscriptionActivated(ctx, buildSubscriptionEvent(event.Type, event.Data.Raw))

	case "customer.subscription.deleted":
		return c.hooks.OnSubscriptionCancelled(ctx, buildSubscriptionEvent(event.Type, event.Data.Raw))

	case "customer.subscription.paused":
		return c.hooks.OnSubscriptionPaused(ctx, buildSubscriptionEvent(event.Type, event.Data.Raw))

	case "customer.subscription.resumed":
		return c.hooks.OnSubscriptionResumed(ctx, buildSubscriptionEvent(event.Type, event.Data.Raw))

	case "customer.subscription.updated":
		ev := buildSubscriptionEvent(event.Type, event.Data.Raw)
		// Detect a pause/resume that arrives as a generic update by inspecting
		// what changed (previous_attributes).
		switch pauseTransition(event.Data.PreviousAttributes, event.Data.Raw) {
		case "paused":
			return c.hooks.OnSubscriptionPaused(ctx, ev)
		case "resumed":
			return c.hooks.OnSubscriptionResumed(ctx, ev)
		default:
			return c.hooks.OnSubscriptionUpdated(ctx, ev)
		}

	default:
		return nil
	}
}

// --- lightweight parse structs (resilient to SDK/API-version drift) ---

type checkoutSessionEntity struct {
	ID              string            `json:"id"`
	Mode            string            `json:"mode"`
	PaymentStatus   string            `json:"payment_status"`
	PaymentIntent   string            `json:"payment_intent"`
	Subscription    string            `json:"subscription"`
	Customer        string            `json:"customer"`
	CustomerEmail   string            `json:"customer_email"`
	AmountTotal     int64             `json:"amount_total"`
	Currency        string            `json:"currency"`
	Metadata        map[string]string `json:"metadata"`
	CustomerDetails struct {
		Email string `json:"email"`
	} `json:"customer_details"`
}

func (s checkoutSessionEntity) email() string {
	if s.CustomerEmail != "" {
		return s.CustomerEmail
	}
	return s.CustomerDetails.Email
}

type paymentIntentEntity struct {
	ID               string            `json:"id"`
	Amount           int64             `json:"amount"`
	Currency         string            `json:"currency"`
	Customer         string            `json:"customer"`
	ReceiptEmail     string            `json:"receipt_email"`
	Invoice          string            `json:"invoice"`
	Metadata         map[string]string `json:"metadata"`
	LastPaymentError *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"last_payment_error"`
}

type invoiceEntity struct {
	ID            string            `json:"id"`
	Customer      string            `json:"customer"`
	Subscription  string            `json:"subscription"`
	PaymentIntent string            `json:"payment_intent"`
	AmountPaid    int64             `json:"amount_paid"`
	Currency      string            `json:"currency"`
	Metadata      map[string]string `json:"metadata"`
	// Newer API versions nest the subscription under parent.subscription_details.
	Parent *struct {
		SubscriptionDetails *struct {
			Subscription string `json:"subscription"`
		} `json:"subscription_details"`
	} `json:"parent"`
	Lines struct {
		Data []struct {
			Price *struct {
				ID string `json:"id"`
			} `json:"price"`
		} `json:"data"`
	} `json:"lines"`
}

func (inv invoiceEntity) subscriptionID() string {
	if inv.Subscription != "" {
		return inv.Subscription
	}
	if inv.Parent != nil && inv.Parent.SubscriptionDetails != nil {
		return inv.Parent.SubscriptionDetails.Subscription
	}
	return ""
}

func (inv invoiceEntity) priceID() string {
	if len(inv.Lines.Data) > 0 && inv.Lines.Data[0].Price != nil {
		return inv.Lines.Data[0].Price.ID
	}
	return ""
}

type subscriptionEntity struct {
	ID                string            `json:"id"`
	Customer          string            `json:"customer"`
	Status            string            `json:"status"`
	CancelAtPeriodEnd bool              `json:"cancel_at_period_end"`
	Metadata          map[string]string `json:"metadata"`
	PauseCollection   json.RawMessage   `json:"pause_collection"`
	Items             struct {
		Data []struct {
			Price *struct {
				ID string `json:"id"`
			} `json:"price"`
		} `json:"data"`
	} `json:"items"`
}

func buildSubscriptionEvent(eventType interface{}, raw []byte) SubscriptionEvent {
	var sub subscriptionEntity
	_ = json.Unmarshal(raw, &sub)

	e := SubscriptionEvent{
		Event:             fmt.Sprintf("%v", eventType),
		SubscriptionID:    sub.ID,
		CustomerID:        sub.Customer,
		Status:            sub.Status,
		CancelAtPeriodEnd: sub.CancelAtPeriodEnd,
		Metadata:          sub.Metadata,
		Interval:          intervalFromMetadata(sub.Metadata),
	}
	if len(sub.Items.Data) > 0 && sub.Items.Data[0].Price != nil {
		e.StripePriceID = sub.Items.Data[0].Price.ID
	}
	return e
}

// pauseTransition classifies a subscription.updated event as a pause, a resume,
// or neither, by comparing pause_collection now vs. in previous_attributes.
func pauseTransition(prev map[string]interface{}, raw []byte) string {
	if prev == nil {
		return ""
	}
	_, changed := prev["pause_collection"]
	if !changed {
		return ""
	}
	var sub subscriptionEntity
	_ = json.Unmarshal(raw, &sub)
	nowPaused := len(sub.PauseCollection) > 0 && string(sub.PauseCollection) != "null"
	if nowPaused {
		return "paused"
	}
	return "resumed"
}

func intervalFromMetadata(md map[string]string) Interval {
	if md == nil {
		return IntervalMonthly
	}
	return Interval(strings.ToLower(strings.TrimSpace(md["interval"]))).Normalize()
}
