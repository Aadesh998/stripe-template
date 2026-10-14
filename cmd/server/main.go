package main

import (
	"log"
	"net/http"
	"os"

	"github.com/example/stripe-template/stripe"
	"github.com/example/stripe-template/transport"
)

func main() {
	cfg, err := stripe.LoadConfigFromEnv()
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	client := stripe.New(cfg, ExampleHooks{})

	mux := http.NewServeMux()
	transport.NewHandler(client).RegisterRoutes(mux)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("stripe template server listening on :%s", port)
	if err := http.ListenAndServe(":"+port, withCORS(mux)); err != nil {
		log.Fatal(err)
	}
}

// withCORS allows all origins for LOCAL DEV ONLY. Lock this down in production.
func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
