package models

func (r Refund) ApprovedDisplay() string {
	if r.ApprovedAmount == nil {
		return ""
	}
	return *r.ApprovedAmount
}

func (d OrderDispute) StatusLabel() string {
	switch d.Status {
	case DisputeSubmitted:
		return "Awaiting response"
	case DisputeSellerResponded:
		return "Seller responded"
	case DisputeUnderReview:
		return "Under admin review"
	case DisputeResolvedRefund:
		return "Refund settled"
	case DisputeResolvedRejected:
		return "Rejected"
	case DisputeWithdrawn:
		return "Withdrawn"
	}
	return "Recorded"
}

func (d OrderDispute) ReasonLabel() string {
	switch d.ReasonCode {
	case "non_delivery", "item_not_received":
		return "Order not received"
	case "damaged_item":
		return "Damaged produce"
	case "wrong_item":
		return "Wrong item"
	case "missing_quantity":
		return "Missing quantity"
	case "quality_issue":
		return "Quality issue"
	}
	return "Other issue"
}

func (r Refund) StatusLabel() string {
	switch r.Status {
	case RefundSucceeded:
		return "Settled to buyer wallet"
	case RefundRequested:
		return "Approved, awaiting settlement"
	case RefundProcessing:
		return "Processing"
	case RefundRequiresReview:
		return "Needs financial review"
	case RefundFailed:
		return "Settlement failed"
	}
	return "Recorded"
}

func (e OrderDisputeEvent) Label() string {
	switch e.EventType {
	case "dispute_submitted":
		return "Dispute submitted"
	case "seller_responded":
		return "Seller response received"
	case "admin_review_started":
		return "Admin review started"
	case "dispute_rejected":
		return "Dispute rejected"
	case "dispute_withdrawn":
		return "Dispute withdrawn"
	case "refund_approved":
		return "Refund approved"
	case "refund_succeeded":
		return "Refund settled"
	case "refund_requires_review":
		return "Financial review needed"
	case "evidence_uploaded":
		return "Evidence uploaded"
	}
	return "Status recorded"
}
