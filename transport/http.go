package transport

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"

	"github.com/example/stripe-template/stripe"
)

type Handler struct {
	client *stripe.Client
}

func NewHandler(client *stripe.Client) *Handler {
	return &Handler{client: client}
}

// RegisterRoutes mounts the endpoints on a stdlib ServeMux:
//
//	POST /api/payments/checkout            -> one-time Checkout Session (hosted)
//	POST /api/payments/intent              -> PaymentIntent (custom Elements UI)
//	POST /api/subscriptions                -> subscription Checkout Session
//	POST /api/subscriptions/cancel         -> cancel (now or at period end)
//	POST /api/subscriptions/pause          -> pause collection
//	POST /api/subscriptions/resume         -> resume collection
//	POST /api/subscriptions/change-plan    -> upgrade/downgrade (prorated)
//	POST /api/subscriptions/portal         -> Billing Portal session
//	GET  /api/subscriptions/{id}           -> fetch live status
//	POST /api/webhooks/stripe              -> webhook receiver
//	GET  /api/config                       -> public publishable key
//
// Uses Go 1.22+ method+path patterns. On another router, copy the handler
// bodies — they only depend on *stripe.Client.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/payments/checkout", h.createCheckout)
	mux.HandleFunc("POST /api/payments/intent", h.createPaymentIntent)
	mux.HandleFunc("POST /api/subscriptions", h.createSubscription)
	mux.HandleFunc("POST /api/subscriptions/cancel", h.cancelSubscription)
	mux.HandleFunc("POST /api/subscriptions/pause", h.pauseSubscription)
	mux.HandleFunc("POST /api/subscriptions/resume", h.resumeSubscription)
	mux.HandleFunc("POST /api/subscriptions/change-plan", h.changePlan)
	mux.HandleFunc("POST /api/subscriptions/portal", h.portal)
	mux.HandleFunc("GET /api/subscriptions/{id}", h.subscriptionStatus)
	mux.HandleFunc("POST /api/webhooks/stripe", h.webhook)
	mux.HandleFunc("GET /api/config", h.config)
}

// --- one-time ---

func (h *Handler) createCheckout(w http.ResponseWriter, r *http.Request) {
	var req stripe.CheckoutRequest
	if !decode(w, r, &req) {
		return
	}
	session, err := h.client.CreateCheckoutSession(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"session":         session,
		"url":             session.URL,
		"publishable_key": h.client.PublishableKey(),
	})
}

func (h *Handler) createPaymentIntent(w http.ResponseWriter, r *http.Request) {
	var req stripe.PaymentIntentRequest
	if !decode(w, r, &req) {
		return
	}
	pi, err := h.client.CreatePaymentIntent(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"client_secret":   pi.ClientSecret,
		"payment_intent":  pi,
		"publishable_key": h.client.PublishableKey(),
	})
}

// --- subscriptions ---

func (h *Handler) createSubscription(w http.ResponseWriter, r *http.Request) {
	var req stripe.SubscriptionRequest
	if !decode(w, r, &req) {
		return
	}
	session, err := h.client.CreateSubscription(r.Context(), req)
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, stripe.ErrPriceNotResolved) {
			status = http.StatusBadRequest
		}
		writeError(w, status, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"session":         session,
		"url":             session.URL,
		"publishable_key": h.client.PublishableKey(),
	})
}

type subActionRequest struct {
	SubscriptionID    string `json:"subscription_id"`
	CancelAtPeriodEnd bool   `json:"cancel_at_period_end"`
	NewPlanID         string `json:"new_plan_id"`
	Interval          string `json:"interval"`
	Prorate           *bool  `json:"prorate"`
}

func (h *Handler) cancelSubscription(w http.ResponseWriter, r *http.Request) {
	var req subActionRequest
	if !decode(w, r, &req) {
		return
	}
	sub, err := h.client.CancelSubscription(req.SubscriptionID, req.CancelAtPeriodEnd)
	respondSub(w, sub, err)
}

func (h *Handler) pauseSubscription(w http.ResponseWriter, r *http.Request) {
	var req subActionRequest
	if !decode(w, r, &req) {
		return
	}
	sub, err := h.client.PauseSubscription(req.SubscriptionID)
	respondSub(w, sub, err)
}

func (h *Handler) resumeSubscription(w http.ResponseWriter, r *http.Request) {
	var req subActionRequest
	if !decode(w, r, &req) {
		return
	}
	sub, err := h.client.ResumeSubscription(req.SubscriptionID)
	respondSub(w, sub, err)
}

func (h *Handler) changePlan(w http.ResponseWriter, r *http.Request) {
	var req subActionRequest
	if !decode(w, r, &req) {
		return
	}
	prorate := true
	if req.Prorate != nil {
		prorate = *req.Prorate
	}
	sub, err := h.client.ChangePlan(r.Context(), req.SubscriptionID, req.NewPlanID, stripe.Interval(req.Interval), prorate)
	if err != nil && errors.Is(err, stripe.ErrPriceNotResolved) {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	respondSub(w, sub, err)
}

type portalRequest struct {
	CustomerID string `json:"customer_id"`
	ReturnURL  string `json:"return_url"`
}

func (h *Handler) portal(w http.ResponseWriter, r *http.Request) {
	var req portalRequest
	if !decode(w, r, &req) {
		return
	}
	ps, err := h.client.CreatePortalSession(r.Context(), stripe.PortalRequest{
		CustomerID: req.CustomerID,
		ReturnURL:  req.ReturnURL,
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, ps)
}

func (h *Handler) subscriptionStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sub, err := h.client.FetchSubscription(id)
	respondSub(w, sub, err)
}

// --- webhook ---

func (h *Handler) webhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	signature := r.Header.Get("Stripe-Signature")

	if err := h.client.HandleWebhook(r.Context(), body, signature); err != nil {
		if errors.Is(err, stripe.ErrInvalidSignature) {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		// 5xx tells Stripe to retry — appropriate for transient hook failures.
		log.Printf("[stripe webhook] handling failed: %v", err)
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) config(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"publishable_key": h.client.PublishableKey()})
}

// --- helpers ---

func decode(w http.ResponseWriter, r *http.Request, dst interface{}) bool {
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, errors.New("invalid JSON body"))
		return false
	}
	return true
}

func respondSub(w http.ResponseWriter, sub *stripe.Subscription, err error) {
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, sub)
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
