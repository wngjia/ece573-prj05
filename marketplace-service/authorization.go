package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"ece573-prj05/auth"
)

type idFilter func(r *http.Request, subject string) (bool, error)
type roleFilter func(role string) bool

func allowRole(expected string) roleFilter {
	return func(role string) bool {
		return role == expected
	}
}

func allowAny() roleFilter {
	return func(_ string) bool {
		return true
	}
}

func matchBody(field string) idFilter {
	return func(r *http.Request, subject string) (bool, error) {
		body, err := io.ReadAll(io.LimitReader(r.Body, (16<<10)+1))
		if err != nil {
			return false, err
		}
		if len(body) > 16<<10 {
			return false, errors.New("identity body too large")
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		var values map[string]json.RawMessage
		if err := json.Unmarshal(body, &values); err != nil {
			return false, err
		}
		var claimedID string
		if err := json.Unmarshal(values[field], &claimedID); err != nil {
			return false, err
		}
		claimedID = strings.TrimSpace(claimedID)
		if claimedID == "" {
			return false, errors.New("missing identity field")
		}
		return claimedID == subject, nil
	}
}

func matchPath(field string) idFilter {
	return func(r *http.Request, subject string) (bool, error) {
		claimedID := strings.TrimSpace(r.PathValue(field))
		if claimedID == "" {
			return false, errors.New("missing identity field")
		}
		return claimedID == subject, nil
	}
}

func matchAny() idFilter {
	return func(_ *http.Request, _ string) (bool, error) {
		return true, nil
	}
}

func requireAuthorization(public ed25519.PublicKey, matchesID idFilter, allowsRole roleFilter, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw, err := auth.Bearer(r.Header.Get("Authorization"))
		if err != nil {
			writeError(w, http.StatusUnauthorized, "valid Bearer token required")
			return
		}
		identity, err := auth.Verify(public, raw)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "valid Bearer token required")
			return
		}
		// TODO Project 3: authorize the verified identity:
		// 1. Call matchesID(r, identity.Subject). If it returns an
		//    error, call writeError(w, http.StatusBadRequest,
		//    "invalid identity field") and return.
		// 2. Check whether the ID matches and allowsRole(identity.Role).
		// 3. If both checks pass, call next(w, r) and return. A valid but
		//    mismatched ID or role falls through to the HTTP 403 below.
		_ = identity
		writeError(w, http.StatusForbidden, "identity or role does not permit this operation")
	}
}
