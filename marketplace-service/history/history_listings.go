package history

import (
	"fmt"
	"time"

	"ece573-prj05/marketplace-service/model"
)

// WriteListing projects one new listing into seller and available views.
func (s *History) WriteListing(listing model.Listing) error {
	listedAt, err := time.Parse(time.RFC3339, listing.ListedAt)
	if err != nil {
		return fmt.Errorf("parse listing timestamp: %w", err)
	}
	if err := s.write(`INSERT INTO marketplace_history.listings_by_seller
		(seller_id, listed_at, listing_id, title, credit_cost, status)
		VALUES (?, ?, ?, ?, ?, ?)`, listing.SellerID, listedAt, listing.ListingID,
		listing.Title, listing.CreditCost, listing.Status); err != nil {
		return fmt.Errorf("write seller listing: %w", err)
	}
	if err := s.write(`INSERT INTO marketplace_history.available_listings
		(feed, listed_at, listing_id, seller_id, title, credit_cost, status)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, "all", listedAt, listing.ListingID,
		listing.SellerID, listing.Title, listing.CreditCost, listing.Status); err != nil {
		return fmt.Errorf("write available listing: %w", err)
	}
	return nil
}

// ListingsBySeller returns all listings created by one seller.
func (s *History) ListingsBySeller(sellerID string) ([]model.Listing, error) {
	return s.readListings(`SELECT listing_id, seller_id, title, credit_cost, status, listed_at
		FROM marketplace_history.listings_by_seller WHERE seller_id = ?`, sellerID)
}

// AvailableListings returns the available browse view, optionally for one seller.
func (s *History) AvailableListings(sellerID string) ([]model.Listing, error) {
	if sellerID != "" {
		listings, err := s.ListingsBySeller(sellerID)
		if err != nil {
			return nil, err
		}
		available := []model.Listing{}
		for _, listing := range listings {
			if listing.Status == "available" {
				available = append(available, listing)
			}
		}
		return available, nil
	}
	return s.readListings(`SELECT listing_id, seller_id, title, credit_cost, status, listed_at
		FROM marketplace_history.available_listings WHERE feed = ?`, "all")
}

func (s *History) readListings(statement string, key string) ([]model.Listing, error) {
	iter := s.session.Query(statement, key).Consistency(s.readConsistency).Iter()
	listings := []model.Listing{}
	for {
		var listing model.Listing
		var listedAt time.Time
		if !iter.Scan(&listing.ListingID, &listing.SellerID, &listing.Title,
			&listing.CreditCost, &listing.Status, &listedAt) {
			break
		}
		listing.ListedAt = listedAt.UTC().Format(time.RFC3339)
		listings = append(listings, listing)
	}
	if err := iter.Close(); err != nil {
		return nil, err
	}
	return listings, nil
}
