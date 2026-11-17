package routes

import (
	"net/http"
	"os"
	"strings"

	"backend/handlers"
	app "backend/internal"
	"backend/middleware"
)

func registerDisputeRoutes(container *app.Container) {
	if container.OrderDispute == nil {
		return
	}
	privateRoot := strings.TrimSpace(os.Getenv("DISPUTE_EVIDENCE_DIR"))
	if privateRoot == "" {
		privateRoot = "private/dispute-evidence"
	}
	h := handlers.NewOrderDisputeHandler(container.OrderDispute, container.Wallet, container.Order, container.Admin, privateRoot)
	auth := middleware.NewAuthenticator(container.FarmerRepo, container.Sessions)
	admin := handlers.NewAdminHandler(container.Admin)
	handle := func(path, method string, fn http.HandlerFunc, review bool) {
		if review {
			fn = admin.DisputeAccess(fn)
		}
		protected := middleware.CSRFProtect(middleware.OnlyPath(path, auth.RequireAuth(fn)))
		http.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			if r.Method != method {
				w.Header().Set("Allow", method)
				http.Error(w, "Method Not Allowed", 405)
				return
			}
			protected.ServeHTTP(w, r)
		})
	}
	for _, route := range []struct {
		path    string
		handler http.HandlerFunc
	}{
		{"/disputes", h.Mine}, {"/api/disputes", h.Mine}, {"/disputes/new", h.New},
		{"/disputes/view", h.Case(false)}, {"/api/disputes/view", h.Case(false)},
		{"/disputes/evidence", h.DownloadEvidence},
	} {
		handle(route.path, "GET", route.handler, false)
	}
	for _, route := range []struct {
		path    string
		handler http.HandlerFunc
	}{
		{"/orders/disputes", h.Create}, {"/api/disputes/create", h.Create},
		{"/orders/disputes/respond", h.Respond}, {"/disputes/withdraw", h.Withdraw},
		{"/disputes/evidence/upload", h.UploadEvidence}, {"/orders/confirm-receipt", h.ConfirmReceipt},
	} {
		handle(route.path, "POST", route.handler, false)
	}
	for _, route := range []struct {
		path    string
		handler http.HandlerFunc
	}{
		{"/admin/disputes", h.Queue(false)}, {"/api/admin/disputes", h.Queue(false)},
		{"/admin/disputes/view", h.Case(true)}, {"/api/admin/disputes/view", h.Case(true)},
		{"/admin/payment-exceptions", h.Queue(true)}, {"/api/admin/payment-exceptions", h.Queue(true)},
	} {
		handle(route.path, "GET", route.handler, true)
	}
	for _, action := range []string{"review", "reject", "approve", "settle"} {
		handle("/admin/disputes/"+action, "POST", h.AdminChange(action), true)
		handle("/api/admin/disputes/"+action, "POST", h.AdminChange(action), true)
	}
}
