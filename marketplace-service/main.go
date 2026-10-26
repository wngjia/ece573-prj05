package main

import (
	"crypto/ed25519"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"ece573-prj05/auth"
	"ece573-prj05/marketplace-service/history"
	"ece573-prj05/marketplace-service/marketplace"
)

func main() {
	store, err := openStore()
	if err != nil {
		log.Fatal("FATAL open Postgres store: ", err)
	}
	defer store.Close()

	history, err := openHistory()
	if err != nil {
		log.Fatal("FATAL open Cassandra history: ", err)
	}
	defer history.Close()

	enabled := strings.ToLower(os.Getenv("AUTH_ENABLED"))
	if enabled == "" {
		enabled = "false"
	}
	if enabled != "true" && enabled != "false" {
		log.Fatal("FATAL AUTH_ENABLED must be true or false")
	}
	var public ed25519.PublicKey
	if enabled == "true" {
		public, err = auth.LoadPublicKey(requiredEnvironment("JWT_PUBLIC_KEY_FILE"))
		if err != nil {
			log.Fatal("FATAL load JWT public key: ", err)
		}
	} else {
		log.Println("WARN authentication disabled; use only for course development")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	if enabled == "false" {
		mux.HandleFunc("POST /listings", createListingHandler(store, history))
		mux.HandleFunc("POST /orders", createOrderHandler(store, history))
		mux.HandleFunc("GET /accounts/{account_id}", accountHandler(store, history))
		mux.HandleFunc("POST /accounts/{account_id}/credits", grantCreditsHandler(store))
	} else {
		mux.HandleFunc("POST /listings", requireAuthorization(
			public, matchBody("seller_id"), allowRole("user"), createListingHandler(store, history),
		))
		// TODO Project 3: use the listings route above as an example and add
		// requireAuthorization-wrapped registrations for these three routes:
		// 1. POST /orders: matchBody("buyer_id"), allowRole("user").
		// 2. GET /accounts/{account_id}: matchPath("account_id"), allowAny().
		// 3. POST /accounts/{account_id}/credits: matchAny(), allowRole("admin").
	}
	mux.HandleFunc("GET /listings", browseListingsHandler(history))

	log.Println("INFO marketplace service listening on :8080")
	log.Fatal("FATAL marketplace HTTP server: ", http.ListenAndServe(":8080", limitRequests(mux)))
}

func openHistory() (*history.History, error) {
	var err error
	config := history.DatabaseConfig{
		ContactPoints:    requiredEnvironment("CASSANDRA_CONTACT_POINTS"),
		ReadConsistency:  requiredEnvironment("READ_CONSISTENCY"),
		WriteConsistency: requiredEnvironment("WRITE_CONSISTENCY"),
	}
	config.ReplicationFactor, err = strconv.Atoi(environmentDefault("CASSANDRA_REPLICATION_FACTOR", "3"))
	if err != nil {
		return nil, fmt.Errorf("CASSANDRA_REPLICATION_FACTOR: %w", err)
	}
	config.WriteRetries, err = strconv.Atoi(environmentDefault("CASSANDRA_WRITE_RETRIES", "0"))
	if err != nil {
		return nil, fmt.Errorf("CASSANDRA_WRITE_RETRIES: %w", err)
	}
	config.RetryInterval, err = time.ParseDuration(environmentDefault("CASSANDRA_RETRY_INTERVAL", "1s"))
	if err != nil {
		return nil, fmt.Errorf("CASSANDRA_RETRY_INTERVAL: %w", err)
	}
	return history.OpenHistory(config)
}

func environmentDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func openStore() (*marketplace.Store, error) {
	config := marketplace.DatabaseConfig{
		Host:     requiredEnvironment("POSTGRES_HOST"),
		Port:     requiredEnvironment("POSTGRES_PORT"),
		Database: requiredEnvironment("POSTGRES_DB"),
		User:     requiredEnvironment("POSTGRES_USER"),
		Password: requiredEnvironment("POSTGRES_PASSWORD"),
	}
	return marketplace.OpenStore(config)
}

func requiredEnvironment(name string) string {
	value := os.Getenv(name)
	if value == "" {
		panic(fmt.Sprintf("required environment variable %s is not set", name))
	}
	return value
}
