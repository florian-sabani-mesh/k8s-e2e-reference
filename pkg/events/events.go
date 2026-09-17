// Package events is the versioned wire contract shared by independently built services.
package events

const Stream = "ORDERS"
const Created = "orders.created"
const Resolved = "price.resolved"
const Failed = "price.failed"

// Envelope IDs identify a logical event, not an individual delivery attempt.
type Envelope struct {
	Version       int    `json:"version"`
	ID            string `json:"id"`
	OrderID       string `json:"order_id"`
	CorrelationID string `json:"correlation_id"`
	Asset         string `json:"asset"`
	Currency      string `json:"currency"`
	Amount        string `json:"amount"`
	Exchange      string `json:"exchange"`
	Price         string `json:"price,omitempty"`
	ErrorCode     string `json:"error_code,omitempty"`
}
