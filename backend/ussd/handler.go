package ussd

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"backend/internal/models"
	"backend/internal/repository"
	"backend/internal/services"
)

type farmerFinder interface {
	GetByPhone(string) (*models.Farmer, error)
}
type cropLister interface {
	AvailableCrops() ([]models.Crop, error)
	MyCrops(int) ([]models.Crop, error)
}
type weatherGetter interface {
	Current(context.Context, string) (services.Weather, error)
}

// Handler implements a provider-neutral USSD callback. It accepts the common
// phoneNumber/text fields used by Africa's Talking and similar gateways.
type Handler struct {
	farmers       farmerFinder
	crops         cropLister
	weather       weatherGetter
	orders        *services.OrderService
	notifications *services.NotificationService
}

func NewHandler(farmers *repository.FarmerRepository, crops *services.CropService, weather *services.WeatherService, orders *services.OrderService, notifications *services.NotificationService) *Handler {
	return &Handler{farmers: farmers, crops: crops, weather: weather, orders: orders, notifications: notifications}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	_ = r.ParseForm()
	phone := strings.TrimSpace(first(r.FormValue("phoneNumber"), r.FormValue("phone"), r.FormValue("msisdn")))
	text := strings.TrimSpace(r.FormValue("text"))
	response := h.respond(r.Context(), phone, text)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(response))
}

func first(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func (h *Handler) respond(ctx context.Context, phone, text string) string {
	// USSD is an authenticated access channel: registration and profile capture
	// happen online, and the gateway phone number identifies that account.
	farmer, err := h.farmers.GetByPhone(phone)
	if err != nil {
		return "END Agro-Shield is temporarily unavailable. Please try again later."
	}
	if farmer == nil {
		return "END This number is not registered. Register online at Agro-Shield, then dial again."
	}
	parts := []string{}
	if text != "" {
		parts = strings.Split(text, "*")
	}
	if len(parts) == 0 || parts[0] == "" {
		return "CON Agro-Shield\n1. Storage\n2. Marketplace\n3. Sales\n4. Notifications\n5. Profile"
	}
	switch parts[0] {
	case "1":
		items, err := h.crops.MyCrops(farmer.ID)
		if err != nil {
			return "END Storage is temporarily unavailable."
		}
		if len(items) == 0 {
			return "END Storage is empty."
		}
		lines := []string{"END Storage:"}
		for _, c := range items {
			lines = append(lines, fmt.Sprintf("%s: %.0f %s", c.Name, c.Quantity, c.Unit))
		}
		return strings.Join(lines[:min(len(lines), 5)], "\n")
	case "2":
		items, err := h.crops.AvailableCrops()
		if err != nil || len(items) == 0 {
			return "END Marketplace is empty."
		}
		lines := []string{"END Marketplace:"}
		for _, c := range items {
			lines = append(lines, fmt.Sprintf("%d. %s %.2f/%s", c.ID, c.Name, c.PricePerUnit, c.Unit))
		}
		return strings.Join(lines[:min(len(lines), 5)], "\n")
	case "3":
		items, err := h.orders.MySales(farmer.ID)
		if err != nil || len(items) == 0 {
			return "END No sales recorded."
		}
		lines := []string{"END Sales:"}
		for _, o := range items {
			lines = append(lines, fmt.Sprintf("%s: %.2f (%s)", o.CropName, o.TotalPrice, o.Status))
		}
		return strings.Join(lines[:min(len(lines), 5)], "\n")
	case "4":
		count, err := h.notifications.GetUnreadCount(farmer.ID)
		if err != nil {
			return "END Notifications unavailable."
		}
		return fmt.Sprintf("END You have %d unread notification(s).", count)
	case "5":
		return fmt.Sprintf("END %s\n%s\n%s", farmer.FullName, farmer.Phone, first(farmer.Location, "Location not set"))
	default:
		return "CON Invalid option\n1. Storage\n2. Marketplace\n3. Sales\n4. Notifications\n5. Profile"
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ParseInt is kept small and exported for integrations that need to parse a
// menu selection consistently.
func ParseInt(value string) (int, error) { return strconv.Atoi(strings.TrimSpace(value)) }
