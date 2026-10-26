package marketplace

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"ece573-prj05/marketplace-service/model"
)

// CreateOrder sells one listing and records the completed order in a database
// transaction.
func (s *Store) CreateOrder(listingID, buyerID string) (model.Order, error) {
	listingID = strings.TrimSpace(listingID)
	buyerID = strings.TrimSpace(buyerID)
	if listingID == "" || buyerID == "" {
		return model.Order{}, ErrInvalidOrder
	}

	tx, err := s.db.BeginTx(context.Background(), &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return model.Order{}, fmt.Errorf("begin checkout: %w", err)
	}
	defer tx.Rollback()

	var listing model.Listing
	var listedAt time.Time
	err = tx.QueryRow(`
		SELECT listing_id, seller_id, title, credit_cost, status, listed_at
		FROM listings
		WHERE listing_id = $1
		FOR UPDATE`,
		listingID,
	).Scan(
		&listing.ListingID,
		&listing.SellerID,
		&listing.Title,
		&listing.CreditCost,
		&listing.Status,
		&listedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return model.Order{}, ErrListingNotFound
	}
	if err != nil {
		return model.Order{}, fmt.Errorf("read listing for checkout: %w", err)
	}
	if listing.Status != "available" {
		return model.Order{}, ErrListingUnavailable
	}

	if listing.SellerID == buyerID {
		return model.Order{}, ErrSelfPurchase
	}

	accountIDs := []string{buyerID, listing.SellerID}
	if accountIDs[0] > accountIDs[1] {
		accountIDs[0], accountIDs[1] = accountIDs[1], accountIDs[0]
	}

	var buyerBalance int
	// Lock both accounts in a consistent order to avoid buyer/seller deadlocks.
	for _, accountID := range accountIDs {
		var balance int
		err = tx.QueryRow(`
			SELECT balance
			FROM accounts
			WHERE account_id = $1
			FOR UPDATE`,
			accountID,
		).Scan(&balance)
		if errors.Is(err, sql.ErrNoRows) {
			return model.Order{}, ErrAccountNotFound
		}
		if err != nil {
			return model.Order{}, fmt.Errorf("lock checkout account: %w", err)
		}
		if accountID == buyerID {
			buyerBalance = balance
		}
	}

	if buyerBalance < listing.CreditCost {
		return model.Order{}, ErrInsufficientCredit
	}

	_, err = tx.Exec(`
		UPDATE accounts
		SET balance = balance - $1
		WHERE account_id = $2`,
		listing.CreditCost,
		buyerID,
	)
	if err != nil {
		return model.Order{}, fmt.Errorf("debit buyer account: %w", err)
	}

	_, err = tx.Exec(`
		UPDATE accounts
		SET balance = balance + $1
		WHERE account_id = $2`,
		listing.CreditCost,
		listing.SellerID,
	)
	if err != nil {
		return model.Order{}, fmt.Errorf("credit seller account: %w", err)
	}

	_, err = tx.Exec(
		"UPDATE listings SET status = 'sold' WHERE listing_id = $1",
		listing.ListingID,
	)
	if err != nil {
		return model.Order{}, fmt.Errorf("sell listing: %w", err)
	}

	order := model.Order{
		ListingID:  listing.ListingID,
		BuyerID:    buyerID,
		SellerID:   listing.SellerID,
		Title:      listing.Title,
		CreditCost: listing.CreditCost,
		ListedAt:   formatTime(listedAt),
	}
	var orderedAt, processedAt time.Time
	err = tx.QueryRow(`
		INSERT INTO orders (listing_id, buyer_id, status, processed_at)
		VALUES ($1, $2, 'completed', statement_timestamp())
		RETURNING order_id, status, ordered_at, processed_at`,
		listing.ListingID,
		buyerID,
	).Scan(
		&order.OrderID,
		&order.Status,
		&orderedAt,
		&processedAt,
	)
	if err != nil {
		return model.Order{}, fmt.Errorf("record order: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return model.Order{}, fmt.Errorf("commit checkout: %w", err)
	}
	order.OrderedAt = formatTime(orderedAt)
	order.ProcessedAt = formatTime(processedAt)
	return order, nil
}
