package models

import (
	"strings"
	"time"
)

type AdminPermission string

const (
	AdminEnter            AdminPermission = "console.read"
	AdminReadUsers        AdminPermission = "users.read"
	AdminManageUsers      AdminPermission = "users.manage"
	AdminManageRoles      AdminPermission = "roles.manage"
	AdminModerateListings AdminPermission = "listings.moderate"
	AdminReadAudit        AdminPermission = "audit.read"
	AdminReviewDisputes   AdminPermission = "disputes.review"
	AdminManageWallet     AdminPermission = "wallet.manage"
)

// Governance roles are separate from the farmer/buyer role and wallet access.
type AdminAccess struct {
	Role       string `json:"role"`
	Wallet     bool   `json:"wallet"`
	Configured bool   `json:"configured"`
}

func (a AdminAccess) Can(p AdminPermission) bool {
	if p == AdminEnter {
		return a.Wallet || a.Role == "administrator" || a.Role == "moderator"
	}
	if p == AdminManageWallet {
		return a.Wallet
	}
	switch p {
	case AdminReadUsers, AdminModerateListings:
		return a.Role == "administrator" || a.Role == "moderator"
	case AdminManageUsers, AdminManageRoles, AdminReadAudit, AdminReviewDisputes:
		return a.Role == "administrator"
	default:
		return false
	}
}

func (a AdminAccess) Label() string {
	if a.Role == "administrator" {
		return "Administrator"
	}
	if a.Role == "moderator" {
		return "Moderator"
	}
	if a.Wallet {
		return "Wallet approver"
	}
	return "Member"
}

func AdminInitials(name string) string {
	parts := strings.Fields(name)
	if len(parts) == 0 {
		return "AS"
	}
	initials := string([]rune(parts[0])[0])
	if len(parts) > 1 {
		initials += string([]rune(parts[len(parts)-1])[0])
	}
	return strings.ToUpper(initials)
}

type AdminUser struct {
	ID          int       `json:"id"`
	FullName    string    `json:"full_name"`
	PhoneHint   string    `json:"phone_hint"`
	Role        string    `json:"role"`
	AdminRole   string    `json:"admin_role"`
	WalletAdmin bool      `json:"wallet_admin"`
	Suspended   bool      `json:"suspended"`
	Configured  bool      `json:"configured"`
	Version     int64     `json:"version"`
	CreatedAt   time.Time `json:"created_at"`
}

func (u AdminUser) Initials() string    { return AdminInitials(u.FullName) }
func (u AdminUser) AccessLabel() string { return (AdminAccess{Role: u.AdminRole}).Label() }

type AdminListing struct {
	ID              int     `json:"id"`
	Name            string  `json:"name"`
	SellerID        int     `json:"seller_id"`
	SellerName      string  `json:"seller_name"`
	Location        string  `json:"location"`
	Quantity        float64 `json:"quantity"`
	Unit            string  `json:"unit"`
	Price           string  `json:"price"`
	Listed          bool    `json:"listed"`
	Blocked         bool    `json:"blocked"`
	SellerSuspended bool    `json:"seller_suspended"`
	Version         int64   `json:"version"`
}

type AdminAuditEvent struct {
	ID        int64     `json:"id"`
	ActorID   int       `json:"actor_id"`
	Action    string    `json:"action"`
	Resource  string    `json:"resource"`
	TargetID  int64     `json:"target_id"`
	Outcome   string    `json:"outcome"`
	Reason    string    `json:"reason"`
	Before    string    `json:"before"`
	After     string    `json:"after"`
	RequestID string    `json:"request_id"`
	CreatedAt time.Time `json:"created_at"`
}

func (e AdminAuditEvent) Label() string {
	labels := map[string]string{
		"user.suspend": "Account suspended", "user.reactivate": "Account reactivated",
		"user.role_none": "Staff role removed", "user.role_moderator": "Moderator assigned",
		"user.role_administrator": "Administrator assigned", "user.wallet_grant": "Wallet access granted",
		"user.wallet_revoke": "Wallet access removed", "listing.block": "Listing blocked",
		"listing.restore": "Listing restored", "wallet.status": "Wallet status changed",
		"wallet.withdrawal.approve": "Withdrawal approval", "wallet.withdrawal.reject": "Withdrawal rejection",
		"wallet.withdrawal.reconcile": "Payout status check", "wallet.withdrawal.return": "Payout returned to queue",
		"wallet.withdrawal.record": "Payout reference recorded", "wallet.transfer.approve": "Transfer approval",
		"wallet.transfer.reject": "Transfer rejection", "wallet.repair": "Wallet repair",
	}
	if label, ok := labels[e.Action]; ok {
		return label
	}
	return e.Action
}

func (e AdminAuditEvent) OutcomeLabel() string {
	switch e.Outcome {
	case "committed":
		return "Applied"
	case "attempted":
		return "Requested"
	case "returned":
		return "Response sent"
	case "failed":
		return "Unsuccessful"
	default:
		return e.Outcome
	}
}

func (e AdminAuditEvent) ReasonLabel() string {
	switch e.Reason {
	case "policy_violation":
		return "Policy violation"
	case "suspected_fraud":
		return "Suspected fraud"
	case "incorrect_listing":
		return "Incorrect listing"
	case "review_cleared":
		return "Review cleared"
	case "staff_assignment":
		return "Staff assignment"
	default:
		return e.Reason
	}
}

type AdminChange struct {
	TargetID int    `json:"target_id"`
	Version  int64  `json:"version"`
	Action   string `json:"action"`
	Reason   string `json:"reason"`
}

type AdminSummary struct {
	Users     int `json:"users"`
	Suspended int `json:"suspended"`
	Listings  int `json:"listings"`
	Blocked   int `json:"blocked"`
}
