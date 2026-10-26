package history

import (
	"fmt"
	"time"

	"ece573-prj05/marketplace-service/model"
)

// WriteOrder projects a completed order and marks its listing sold.
func (s *History) WriteOrder(order model.Order) error {
	orderedAt, err := time.Parse(time.RFC3339, order.OrderedAt)
	if err != nil {
		return fmt.Errorf("parse order timestamp: %w", err)
	}
	listedAt, err := time.Parse(time.RFC3339, order.ListedAt)
	if err != nil {
		return fmt.Errorf("parse listing timestamp: %w", err)
	}
	processedAt, err := time.Parse(time.RFC3339, order.ProcessedAt)
	if err != nil {
		return fmt.Errorf("parse processed timestamp: %w", err)
	}

	day := orderedAt.UTC().Format("2006-01-02")
	if err := s.write(`INSERT INTO marketplace_history.purchases_by_buyer
		(buyer_id, order_day, ordered_at, order_id, listing_id, seller_id, title, credit_cost, status, listed_at, processed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		order.BuyerID, day, orderedAt, order.OrderID, order.ListingID, order.SellerID,
		order.Title, order.CreditCost, order.Status, listedAt, processedAt); err != nil {
		return fmt.Errorf("write buyer history: %w", err)
	}
	if err := s.write(`INSERT INTO marketplace_history.sales_by_seller
		(seller_id, order_day, ordered_at, order_id, listing_id, buyer_id, title, credit_cost, status, listed_at, processed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		order.SellerID, day, orderedAt, order.OrderID, order.ListingID, order.BuyerID,
		order.Title, order.CreditCost, order.Status, listedAt, processedAt); err != nil {
		return fmt.Errorf("write seller history: %w", err)
	}
	if err := s.write(`INSERT INTO marketplace_history.listings_by_seller
		(seller_id, listed_at, listing_id, title, credit_cost, status)
		VALUES (?, ?, ?, ?, ?, 'sold')`,
		order.SellerID, listedAt, order.ListingID, order.Title, order.CreditCost); err != nil {
		return fmt.Errorf("mark seller listing sold: %w", err)
	}
	if err := s.write(`DELETE FROM marketplace_history.available_listings
		WHERE feed = 'all' AND listed_at = ? AND listing_id = ?`,
		listedAt, order.ListingID); err != nil {
		return fmt.Errorf("remove sold listing: %w", err)
	}
	return nil
}

// Purchases returns the buyer's orders in an optional inclusive UTC day range.
func (s *History) Purchases(buyerID, from, to string) ([]model.Order, error) {
	where, args := dayRange("buyer_id", buyerID, from, to)
	return s.readOrders(`SELECT order_id, listing_id, buyer_id, seller_id, title, credit_cost, status, listed_at, ordered_at, processed_at
		FROM marketplace_history.purchases_by_buyer `+where, args)
}

// Sales returns the seller's orders in an optional inclusive UTC day range.
func (s *History) Sales(sellerID, from, to string) ([]model.Order, error) {
	where, args := dayRange("seller_id", sellerID, from, to)
	return s.readOrders(`SELECT order_id, listing_id, buyer_id, seller_id, title, credit_cost, status, listed_at, ordered_at, processed_at
		FROM marketplace_history.sales_by_seller `+where, args)
}

func dayRange(column, accountID, from, to string) (string, []any) {
	where := "WHERE " + column + " = ?"
	args := []any{accountID}
	if from != "" {
		where += " AND order_day >= ?"
		args = append(args, from)
	}
	if to != "" {
		where += " AND order_day <= ?"
		args = append(args, to)
	}
	return where, args
}

func (s *History) readOrders(statement string, args []any) ([]model.Order, error) {
	iter := s.session.Query(statement, args...).Consistency(s.readConsistency).Iter()

	orders := []model.Order{}
	for {
		var order model.Order
		var listedAt, orderedAt, processedAt time.Time
		if !iter.Scan(&order.OrderID, &order.ListingID, &order.BuyerID, &order.SellerID,
			&order.Title, &order.CreditCost, &order.Status, &listedAt, &orderedAt, &processedAt) {
			break
		}
		order.ListedAt = listedAt.UTC().Format(time.RFC3339)
		order.OrderedAt = orderedAt.UTC().Format(time.RFC3339)
		order.ProcessedAt = processedAt.UTC().Format(time.RFC3339)
		orders = append(orders, order)
	}
	if err := iter.Close(); err != nil {
		return nil, err
	}
	return orders, nil
}
