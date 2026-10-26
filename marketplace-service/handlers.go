package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"ece573-prj05/marketplace-service/history"
	"ece573-prj05/marketplace-service/marketplace"
)

type createListingRequest struct {
	SellerID   string `json:"seller_id"`
	Title      string `json:"title"`
	CreditCost int    `json:"credit_cost"`
}

type grantCreditsRequest struct {
	Amount int `json:"amount"`
}

type createOrderRequest struct {
	ListingID string `json:"listing_id"`
	BuyerID   string `json:"buyer_id"`
}

func grantCreditsHandler(store *marketplace.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var request grantCreditsRequest
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON request")
			return
		}
		accountID := strings.TrimSpace(r.PathValue("account_id"))
		balance, err := store.GrantCredits(accountID, request.Amount)
		if err != nil {
			writeMarketplaceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"account_id": accountID, "balance": balance})
	}
}

func createListingHandler(store *marketplace.Store, history *history.History) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var request createListingRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON request")
			return
		}

		listing, err := store.CreateListing(request.SellerID, request.Title, request.CreditCost)
		if err != nil {
			writeMarketplaceError(w, err)
			return
		}
		if err := history.WriteListing(listing); err != nil {
			log.Printf("ERROR event=listing_projection_failed listing_id=%s status=201 error=%v", listing.ListingID, err)
		} else {
			log.Printf("INFO event=listing_projected listing_id=%s status=201", listing.ListingID)
		}
		writeJSON(w, http.StatusCreated, listing)
	}
}

func browseListingsHandler(history *history.History) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sellerID := strings.TrimSpace(r.URL.Query().Get("seller_id"))
		listings, err := history.AvailableListings(sellerID)
		if err != nil {
			writeHistoryError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"listings": listings})
	}
}

func createOrderHandler(store *marketplace.Store, history *history.History) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var request createOrderRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON request")
			return
		}

		order, err := store.CreateOrder(request.ListingID, request.BuyerID)
		if err != nil {
			writeMarketplaceError(w, err)
			return
		}
		if err := history.WriteOrder(order); err != nil {
			log.Printf("ERROR order %s committed in Postgres but Cassandra projection failed: %v", order.OrderID, err)
		} else {
			log.Printf("INFO order %s committed in Postgres and projected to Cassandra", order.OrderID)
		}
		writeJSON(w, http.StatusCreated, order)
	}
}

func accountHandler(store *marketplace.Store, history *history.History) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		accountID := strings.TrimSpace(r.PathValue("account_id"))
		from, to, err := accountDateRange(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, "from and to must be YYYY-MM-DD dates with from <= to")
			return
		}
		balance, err := store.GetBalance(accountID)
		if err != nil {
			writeMarketplaceError(w, err)
			return
		}
		listings, err := history.ListingsBySeller(accountID)
		if err != nil {
			writeHistoryError(w, err)
			return
		}
		orders, err := history.Purchases(accountID, from, to)
		if err != nil {
			writeHistoryError(w, err)
			return
		}
		sales, err := history.Sales(accountID, from, to)
		if err != nil {
			writeHistoryError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"account_id": accountID,
			"balance":    balance,
			"listings":   listings,
			"orders":     orders,
			"sales":      sales,
		})
	}
}

func accountDateRange(r *http.Request) (string, string, error) {
	from := strings.TrimSpace(r.URL.Query().Get("from"))
	to := strings.TrimSpace(r.URL.Query().Get("to"))
	for _, value := range []string{from, to} {
		if value == "" {
			continue
		}
		day, err := time.Parse("2006-01-02", value)
		if err != nil || day.Format("2006-01-02") != value {
			return "", "", errors.New("invalid account date range")
		}
	}
	if from != "" && to != "" && from > to {
		return "", "", errors.New("invalid account date range")
	}
	return from, to, nil
}

func writeHistoryError(w http.ResponseWriter, err error) {
	log.Printf("ERROR Cassandra view read failed (HTTP 503): %v", err)
	writeError(w, http.StatusServiceUnavailable, "history service unavailable")
}

func writeMarketplaceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, marketplace.ErrInvalidListing),
		errors.Is(err, marketplace.ErrInvalidOrder),
		errors.Is(err, marketplace.ErrInvalidAccount),
		errors.Is(err, marketplace.ErrInvalidCreditGrant),
		errors.Is(err, marketplace.ErrCreditTargetNotUser):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, marketplace.ErrAccountNotFound),
		errors.Is(err, marketplace.ErrListingNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, marketplace.ErrListingUnavailable),
		errors.Is(err, marketplace.ErrInsufficientCredit),
		errors.Is(err, marketplace.ErrSelfPurchase):
		writeError(w, http.StatusConflict, err.Error())
	default:
		log.Printf("ERROR marketplace operation failed: %v", err)
		writeError(w, http.StatusInternalServerError, "marketplace operation failed")
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
