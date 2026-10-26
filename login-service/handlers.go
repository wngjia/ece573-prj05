package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	"ece573-prj05/auth"
)

type createAccountRequest struct {
	AccountID string `json:"account_id"`
	Password  string `json:"password"`
}

type loginRequest struct {
	AccountID string `json:"account_id"`
	Password  string `json:"password"`
}

func createAccountHandler(svc *service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var request createAccountRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON request")
			return
		}
		request.AccountID = strings.TrimSpace(request.AccountID)
		if request.AccountID == "" || !validPassword(request.Password) {
			writeError(w, http.StatusBadRequest, "account_id and password of at least eight characters are required")
			return
		}
		if err := svc.registerAccount(r.Context(), request.AccountID, request.Password); err != nil {
			writeInsertError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"account_id": request.AccountID, "balance": 0, "role": "user"})
	}
}

func writeInsertError(w http.ResponseWriter, err error) {
	if errors.Is(err, errAccountExists) {
		writeError(w, http.StatusConflict, "account already exists")
		return
	}
	log.Printf("ERROR account insertion failed: %v", err)
	writeError(w, http.StatusInternalServerError, "cannot create account")
}

func loginHandler(svc *service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var request loginRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON request")
			return
		}
		accountID := strings.TrimSpace(request.AccountID)
		if accountID == "" || request.Password == "" {
			writeError(w, http.StatusUnauthorized, "invalid account or password")
			return
		}
		role, err := svc.authenticate(accountID, request.Password)
		if errors.Is(err, errInvalidCredentials) {
			writeError(w, http.StatusUnauthorized, "invalid account or password")
			return
		}
		if err != nil {
			log.Printf("ERROR authenticate account %s: %v", accountID, err)
			writeError(w, http.StatusInternalServerError, "login unavailable")
			return
		}
		token, err := auth.Issue(svc.private, auth.Identity{Subject: accountID, Role: role})
		if err != nil {
			log.Printf("ERROR issue token for account %s: %v", accountID, err)
			writeError(w, http.StatusInternalServerError, "login unavailable")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"access_token": token, "token_type": "Bearer", "expires_in": int(auth.TokenLifetime.Seconds())})
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("ERROR cannot encode response: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
