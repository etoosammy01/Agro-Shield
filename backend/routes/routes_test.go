package routes

import (
	"os"
	"strings"
	"testing"
)

func TestDashboardLinksHaveRegisteredRoutes(t *testing.T) {
	content, err := os.ReadFile("../../frontend/pages/dashboard.html")
	if err != nil {
		t.Fatal(err)
	}
	page := string(content)
	for _, href := range []string{"/dashboard", "/storage", "/marketplace", "/market-insights", "/cart", "/ai-assistant", "/ai-diagnosis-history", "/profile", "/logout"} {
		if !strings.Contains(page, `href="`+href+`"`) {
			t.Errorf("dashboard does not contain expected link %s", href)
		}
	}
}
