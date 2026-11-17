package routes

import (
	"net/http"

	"backend/handlers"
	app "backend/internal"
	"backend/middleware"
)

// Every administrative route gets authentication and CSRF protection here,
// including the existing wallet operations and JSON endpoints.
func registerAdminRoutes(container *app.Container, wallet *handlers.WalletHandler) {
	authenticator := middleware.NewAuthenticator(container.FarmerRepo, container.Sessions)
	admin := handlers.NewAdminHandler(container.Admin)
	handle := func(path string, handler http.HandlerFunc) {
		http.Handle(path, middleware.CSRFProtect(middleware.OnlyPath(path,
			authenticator.RequireAuth(handler))))
	}
	handle("/admin", admin.Page("overview"))
	handle("/api/admin/overview", admin.Page("overview"))
	for _, section := range []string{"users", "listings", "audit"} {
		handle("/admin/"+section, admin.Page(section))
		handle("/api/admin/"+section, admin.Page(section))
	}
	for _, resource := range []string{"users", "listings"} {
		handle("/admin/"+resource+"/change", admin.Change(resource))
		handle("/api/admin/"+resource+"/change", admin.Change(resource))
	}
	// The old role-management URL now enforces the governance contract. Wallet
	// approvers cannot grant access; changes require a version and a reason.
	handle("/admin/wallet-admins", admin.Change("users"))
	handle("/admin/wallet-withdrawals", admin.WalletAccess(wallet.AdminPage, "", "", ""))
	for _, route := range []struct {
		path, action, resource, field string
		handler                       http.HandlerFunc
	}{
		{"/admin/wallet-withdrawals/approve", "wallet.withdrawal.approve", "wallet_withdrawals", "withdrawal_id", wallet.ApproveWithdrawal},
		{"/admin/wallet-withdrawals/reject", "wallet.withdrawal.reject", "wallet_withdrawals", "withdrawal_id", wallet.RejectWithdrawal},
		{"/admin/wallet-withdrawals/reconcile", "wallet.withdrawal.reconcile", "wallet_withdrawals", "withdrawal_id", wallet.ReconcileWithdrawal},
		{"/admin/wallet-withdrawals/return-to-queue", "wallet.withdrawal.return", "wallet_withdrawals", "withdrawal_id", wallet.ReturnWithdrawalToQueue},
		{"/admin/wallet-withdrawals/record-transfer", "wallet.withdrawal.record", "wallet_withdrawals", "withdrawal_id", wallet.RecordTransferID},
		{"/admin/wallet-transfers/approve", "wallet.transfer.approve", "wallet_transfers", "transfer_id", wallet.ApproveTransfer},
		{"/admin/wallet-transfers/reject", "wallet.transfer.reject", "wallet_transfers", "transfer_id", wallet.RejectTransfer},
		{"/admin/wallets/repair", "wallet.repair", "farmers", "user_id", wallet.RepairWallet},
	} {
		handle(route.path, admin.WalletAccess(route.handler, route.action, route.resource, route.field))
	}
}
