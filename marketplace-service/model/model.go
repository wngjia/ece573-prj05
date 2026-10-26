// Package model contains listing and order data shared by the stores.
package model

// Listing is one item offered by a seller.
type Listing struct {
	ListingID  string `json:"listing_id"`
	SellerID   string `json:"seller_id"`
	Title      string `json:"title"`
	CreditCost int    `json:"credit_cost"`
	Status     string `json:"status"`
	ListedAt   string `json:"listed_at"`
}

// Order records a buyer's request and its outcome. A completed order is a
// purchase for the buyer and a sale for the seller.
type Order struct {
	OrderID     string `json:"order_id"`
	ListingID   string `json:"listing_id"`
	BuyerID     string `json:"buyer_id"`
	SellerID    string `json:"seller_id"`
	Title       string `json:"title"`
	CreditCost  int    `json:"credit_cost"`
	Status      string `json:"status"`
	ListedAt    string `json:"listed_at"`
	OrderedAt   string `json:"ordered_at"`
	ProcessedAt string `json:"processed_at"`
}
