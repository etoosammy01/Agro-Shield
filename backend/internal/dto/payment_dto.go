package dto

// InitiatePaymentRequest is the body the frontend sends to POST /orders/pay.
type InitiatePaymentRequest struct {
	OrderID       int64  `json:"order_id"`
	Amount        string `json:"amount"`   // e.g. "5000.00" - keep as string, never float, for money
	Currency      string `json:"currency"` // e.g. "NGN"
	CustomerEmail string `json:"customer_email"`
	CustomerName  string `json:"customer_name"`
}

// InitiatePaymentResponse is what we hand back to the frontend: a link to
// redirect the customer to, and the reference to track the attempt by.
type InitiatePaymentResponse struct {
	CheckoutURL string `json:"checkout_url"`
	Reference   string `json:"reference"`
}

// FlutterwaveWebhookPayload is the shape of what Flutterwave POSTs to our
// webhook endpoint. Only the fields we actually use are mapped here.
type FlutterwaveWebhookPayload struct {
	Event string `json:"event"`
	Data  struct {
		ID     int64  `json:"id"`
		TxRef  string `json:"tx_ref"`
		Status string `json:"status"`
	} `json:"data"`
}
