package main

import (
	"log"
	"net/http"
	"strconv"
	"time"
)

// limitRequests bounds concurrent marketplace requests within one Pod.
// Health checks bypass the limit so load does not make Kubernetes remove
// an otherwise healthy Pod from the Service.
func limitRequests(next http.Handler) http.Handler {
	limit, err := strconv.Atoi(environmentDefault("MARKETPLACE_MAX_INFLIGHT", "8"))
	if err != nil || limit < 1 {
		log.Fatal("FATAL MARKETPLACE_MAX_INFLIGHT must be a positive integer")
	}
	maxWaiting, err := strconv.Atoi(environmentDefault("MARKETPLACE_MAX_WAITING", "8"))
	if err != nil || maxWaiting < 0 {
		log.Fatal("FATAL MARKETPLACE_MAX_WAITING must be a nonnegative integer")
	}
	wait, err := time.ParseDuration(environmentDefault("MARKETPLACE_QUEUE_WAIT", "2s"))
	if err != nil || wait < 0 {
		log.Fatal("FATAL MARKETPLACE_QUEUE_WAIT must be a nonnegative duration")
	}
	slots := make(chan struct{}, limit)
	admitted := make(chan struct{}, limit+maxWaiting)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		select {
		case admitted <- struct{}{}:
			defer func() { <-admitted }()
		default:
			log.Printf("WARN event=request_rejected reason=capacity status=503 method=%s path=%q max_inflight=%d max_waiting=%d", r.Method, r.URL.Path, limit, maxWaiting)
			writeError(w, http.StatusServiceUnavailable, "marketplace service busy")
			return
		}
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
			next.ServeHTTP(w, r)
		case <-timer.C:
			log.Printf("WARN event=request_rejected reason=queue_timeout status=503 method=%s path=%q queue_wait=%s", r.Method, r.URL.Path, wait)
			writeError(w, http.StatusServiceUnavailable, "marketplace service busy")
		case <-r.Context().Done():
			return
		}
	})
}
