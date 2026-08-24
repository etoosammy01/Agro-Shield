package handlers

import (
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"backend/internal/models"
	"backend/internal/repository"
	"backend/internal/services"
	"backend/middleware"
	"backend/render"
)

type Dashboard struct {
	crop         *services.CropService
	order        *services.OrderService
	cart         *services.CartService
	ai           *services.AIService
	negotiation  *services.NegotiationService
	weather      *services.WeatherService
	notification *services.NotificationService
	events       *repository.MarketEventRepository
}

func NewDashboardHandler(crop *services.CropService, order *services.OrderService, cart *services.CartService, ai *services.AIService, negotiation *services.NegotiationService, weather *services.WeatherService, notification *services.NotificationService, events *repository.MarketEventRepository) *Dashboard {
	return &Dashboard{crop: crop, order: order, cart: cart, ai: ai, negotiation: negotiation, weather: weather, notification: notification, events: events}
}

// DashboardData is what dashboard.html renders against. Farmer-only fields
// and buyer-only fields are both here; the template shows the right set
// based on IsBuyer.
type DashboardData struct {
	FullName string
	Role     string
	IsBuyer  bool
	PhotoURL string

	// Farmer stats
	ProduceInStorage     int
	StorageCrops         []models.Crop
	ListingsActive       int
	AIDiagnosesThisMonth int
	Revenue              float64

	// Buyer stats
	PurchasesMade        int
	TotalSpent           float64
	AvailableProduce     int
	CartItemCount        int
	CartTotal            float64
	HealthScore          int
	HealthStatus         string
	HealthMetrics        []HealthMetric
	Notifications        []DashboardNotification
	UnreadNotifications  int
	Weather              services.Weather
	LatestDiagnosis      *models.Diagnosis
	MarketListings       int
	MarketPriceMin       float64
	MarketPriceMedian    float64
	MarketPriceMax       float64
	MarketHasPrices      bool
	MarketOrders30       int
	MarketNegotiations30 int
	Demand               models.ProduceDemandSummary
	ProduceDemands       []models.ProduceDemandSummary
	ProduceDemandPreview []models.ProduceDemandSummary

	Priorities      []DashboardPriority
	PrimaryPriority DashboardPriority
	RecentOrders    []models.Order
}

type HealthMetric struct {
	Name    string
	Percent int
}
type DashboardNotification struct{ Tone, Title, Detail, Href string }

type DashboardPriority struct {
	Tone, Label, Title, Detail, Action, Href string
}

func (h *Dashboard) DashBoard(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		log.Println("User Visited Dashboard")

		data := DashboardData{FullName: "user"}
		if h.events != nil {
			if err := h.events.ReconcileCheckoutAbandonments(24 * time.Hour); err != nil {
				log.Printf("dashboard checkout reconciliation failed: %v", err)
			}
		}

		farmer, ok := middleware.FarmerFromContext(r)
		if ok && farmer != nil {
			data.FullName = farmer.FullName
			data.Role = farmer.Role
			data.IsBuyer = farmer.IsBuyer()
			data.PhotoURL = farmer.PhotoURL
			if h.weather != nil {
				var err error
				data.Weather, err = h.weather.Current(r.Context(), farmer.Location)
				if err != nil {
					if data.Weather.Available {
						log.Printf("dashboard live weather refresh failed for %q; using cached conditions: %v", farmer.Location, err)
					} else {
						log.Printf("dashboard weather unavailable for %q: %v", farmer.Location, err)
					}
				}
			}

			if farmer.IsBuyer() {
				if h.cart != nil {
					if items, err := h.cart.MyCart(farmer.ID); err != nil {
						log.Printf("dashboard cart summary unavailable for buyer %d: %v", farmer.ID, err)
					} else {
						data.CartItemCount, data.CartTotal = cartSummary(items)
					}
				}
				if available, err := h.crop.AvailableCrops(); err == nil {
					data.AvailableProduce = len(available)
				}
				if purchases, err := h.order.MyPurchases(farmer.ID); err == nil {
					data.PurchasesMade = len(purchases)
					data.RecentOrders = limitOrders(purchases, 5)
					for _, p := range purchases {
						data.TotalSpent += p.TotalPrice
					}
				}
				data.Priorities = buyerPriorities(data)
			} else {
				var farmerCrops []models.Crop
				if got, err := h.crop.MyCrops(farmer.ID); err == nil {
					farmerCrops = got
					data.StorageCrops = got
					data.ProduceInStorage = len(got)
					for _, crop := range got {
						if crop.ListedForSale {
							data.ListingsActive++
						}
					}
				}
				if h.events != nil && len(farmerCrops) > 0 {
					cutoff := time.Now().Add(-30 * 24 * time.Hour)
					previous := cutoff.Add(-30 * 24 * time.Hour)
					if summary, err := h.events.FarmerDemandSummary(farmer.ID, farmer.Location, cutoff, previous); err != nil {
						log.Printf("dashboard demand summary unavailable for farmer %d: %v", farmer.ID, err)
					} else {
						data.Demand = summary
					}
					if summaries, err := h.events.GroupedDemandSummaries(farmer.ID, farmer.Location, cutoff, previous); err != nil {
						log.Printf("dashboard produce comparisons unavailable for farmer %d: %v", farmer.ID, err)
					} else {
						data.ProduceDemands = summaries
						data.ProduceDemandPreview = prioritizeDemandSummaries(summaries, 3)
					}
				}
				if available, err := h.crop.AvailableCrops(); err == nil {
					data.MarketListings = len(available)
					prices := make([]float64, 0, len(available))
					for _, listing := range available {
						if listing.PricePerUnit <= 0 {
							continue
						}
						prices = append(prices, listing.PricePerUnit)
					}
					if len(prices) > 0 {
						sort.Float64s(prices)
						data.MarketPriceMin, data.MarketPriceMax = prices[0], prices[len(prices)-1]
						middle := len(prices) / 2
						if len(prices)%2 == 0 {
							data.MarketPriceMedian = (prices[middle-1] + prices[middle]) / 2
						} else {
							data.MarketPriceMedian = prices[middle]
						}
						data.MarketHasPrices = true
					}
				}
				if count, err := h.ai.CountThisMonth(farmer.ID); err == nil {
					data.AIDiagnosesThisMonth = count
				}
				if sales, err := h.order.MySales(farmer.ID); err == nil {
					data.RecentOrders = limitOrders(sales, 5)
					cutoff := time.Now().Add(-30 * 24 * time.Hour)
					for _, s := range sales {
						data.Revenue += s.TotalPrice
						if !s.CreatedAt.Before(cutoff) {
							data.MarketOrders30++
						}
					}
				}
				if h.negotiation != nil {
					if negotiations, err := h.negotiation.MyNegotiations(farmer.ID); err == nil {
						cutoff := time.Now().Add(-30 * 24 * time.Hour)
						for _, n := range negotiations {
							if !n.CreatedAt.Before(cutoff) {
								data.MarketNegotiations30++
							}
						}
					}
				}
				var crops []models.Crop
				if got, err := h.crop.MyCrops(farmer.ID); err == nil {
					crops = got
				}
				var diagnoses []models.Diagnosis
				if got, err := h.ai.History(farmer.ID); err == nil {
					diagnoses = got
					if len(diagnoses) > 0 {
						latest := diagnoses[0]
						data.LatestDiagnosis = &latest
					}
				}
				var negotiations []models.Negotiation
				if h.negotiation != nil {
					if got, err := h.negotiation.MyNegotiations(farmer.ID); err == nil {
						negotiations = got
					}
				}
				data.Priorities = farmerPriorities(data, crops, diagnoses, negotiations)
				data.HealthMetrics, data.HealthScore, data.HealthStatus = farmHealth(data, diagnoses)
			}
			data.Notifications = dashboardNotifications(data, farmer.IsBuyer())
			if h.notification != nil {
				if count, err := h.notification.GetUnreadCount(farmer.ID); err == nil {
					data.UnreadNotifications = count
				}
			}
			if len(data.Priorities) > 0 {
				data.PrimaryPriority = data.Priorities[0]
			}
		}

		if err := render.RenderTemplates(w, "dashboard.html", data); err != nil {
			log.Println("render error", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
	case http.MethodPost:
		log.Println("user's Choices")
	}
}

func cartSummary(items []models.CartItem) (int, float64) {
	total := 0.0
	for _, item := range items {
		total += item.TotalPrice
	}
	return len(items), total
}

func (h *Dashboard) MarketInsights(w http.ResponseWriter, r *http.Request) {
	farmer, ok := middleware.FarmerFromContext(r)
	if !ok || farmer == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	cutoff := time.Now().Add(-30 * 24 * time.Hour)
	summaries, err := h.events.GroupedDemandSummaries(farmer.ID, farmer.Location, cutoff, cutoff.Add(-30*24*time.Hour))
	if err != nil {
		log.Printf("market insights unavailable for farmer %d: %v", farmer.ID, err)
		http.Error(w, "Unable to load market insights", http.StatusInternalServerError)
		return
	}
	data := struct {
		FullName  string
		Summaries []models.ProduceDemandSummary
	}{farmer.FullName, prioritizeDemandSummaries(summaries, len(summaries))}
	if err := render.RenderTemplates(w, "market-insights.html", data); err != nil {
		log.Printf("market insights render error: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func prioritizeDemandSummaries(items []models.ProduceDemandSummary, limit int) []models.ProduceDemandSummary {
	if limit <= 0 || len(items) == 0 {
		return nil
	}
	result := append([]models.ProduceDemandSummary(nil), items...)
	sort.SliceStable(result, func(i, j int) bool {
		rank := func(s models.ProduceDemandSummary) int {
			if s.PriceDifferencePercent > 15 && s.Orders == 0 {
				return 5
			}
			if s.CartAdds > 0 && s.Orders == 0 {
				return 4
			}
			if s.Views+s.CartAdds+s.Orders+s.Negotiations == 0 {
				return 3
			}
			if s.Orders > 0 {
				return 1
			}
			return 2
		}
		return rank(result[i]) > rank(result[j])
	})
	if len(result) > limit {
		result = result[:limit]
	}
	return result
}

func limitOrders(orders []models.Order, max int) []models.Order {
	if len(orders) <= max {
		return orders
	}
	return orders[:max]
}

func buyerPriorities(data DashboardData) []DashboardPriority {
	if data.PurchasesMade == 0 {
		return []DashboardPriority{{Tone: "info", Label: "Get started", Title: "Find your first produce listing", Detail: "Browse fresh produce from local farmers and place an order.", Action: "Browse marketplace", Href: "/marketplace"}}
	}
	return []DashboardPriority{{Tone: "success", Label: "On track", Title: "Your buying activity is up to date", Detail: "Review recent orders or discover something new in the marketplace.", Action: "View marketplace", Href: "/marketplace"}}
}

func farmerPriorities(data DashboardData, crops []models.Crop, diagnoses []models.Diagnosis, negotiations []models.Negotiation) []DashboardPriority {
	var p []DashboardPriority
	for _, d := range diagnoses {
		text := strings.ToLower(d.Result + " " + d.Description)
		if strings.Contains(text, "severe") || strings.Contains(text, "urgent") || strings.Contains(text, "critical") {
			p = append(p, DashboardPriority{"danger", "Urgent", "Review a crop health alert", "Open your latest urgent AI diagnosis and follow its treatment guidance.", "Review diagnosis", "/ai-diagnosis-history"})
			break
		}
	}
	open := 0
	for _, n := range negotiations {
		if n.Status == "open" && !n.IsExpired() {
			open++
		}
	}
	if open > 0 {
		p = append(p, DashboardPriority{"warning", "Response needed", "Reply to a buyer negotiation", "A buyer is waiting for your response.", "View negotiations", "/negotiations"})
	}
	aging := 0
	for _, c := range crops {
		if c.Quantity > 0 && !c.ListedForSale && !c.CreatedAt.IsZero() && time.Since(c.CreatedAt) >= 7*24*time.Hour {
			aging++
		}
	}
	if aging > 0 {
		p = append(p, DashboardPriority{"warning", "Action needed", "List aging produce", "Some stored produce has been waiting for a buyer for more than a week.", "Manage storage", "/storage"})
	}
	if data.ProduceInStorage == 0 {
		p = append(p, DashboardPriority{"warning", "Action needed", "Add produce to your storage", "Create a crop record so you can track stock and list it for sale.", "Open storage", "/storage"})
	}
	if data.ProduceInStorage > 0 && data.ListingsActive == 0 {
		p = append(p, DashboardPriority{"warning", "Action needed", "Publish a listing", "Make your available produce visible to buyers in the marketplace.", "Manage storage", "/storage"})
	}
	if len(p) == 0 {
		p = append(p, DashboardPriority{"success", "On track", "Your farm is ready for business", "Keep stock current and use the AI assistant whenever a crop needs attention.", "View listings", "/marketplace"})
	}
	sort.SliceStable(p, func(i, j int) bool {
		rank := map[string]int{"danger": 3, "warning": 2, "info": 1, "success": 0}
		return rank[p[i].Tone] > rank[p[j].Tone]
	})
	if len(p) > 3 {
		p = p[:3]
	}
	return p
}

func farmHealth(data DashboardData, diagnoses []models.Diagnosis) ([]HealthMetric, int, string) {
	storage, listings, sales, health := 0, 0, 0, 100
	if data.ProduceInStorage > 0 {
		storage = 100
		listings = data.ListingsActive * 100 / data.ProduceInStorage
		if listings > 100 {
			listings = 100
		}
	}
	if data.Revenue > 0 {
		sales = 100
	}
	for _, d := range diagnoses {
		text := strings.ToLower(d.Result + " " + d.Description)
		if strings.Contains(text, "severe") || strings.Contains(text, "urgent") || strings.Contains(text, "critical") {
			health = 40
			break
		}
		if health > 80 {
			health = 85
		}
	}
	score := (storage + listings + sales + health) / 4
	status := "Needs attention"
	if score >= 80 {
		status = "Healthy"
	} else if score >= 60 {
		status = "Fair"
	}
	return []HealthMetric{{"Storage", storage}, {"Listings", listings}, {"Produce health", health}, {"Sales activity", sales}}, score, status
}

func dashboardNotifications(data DashboardData, buyer bool) []DashboardNotification {
	if buyer {
		if data.PurchasesMade == 0 {
			return nil
		}
		return []DashboardNotification{{"success", "Purchase history is up to date", "Review your recent orders or browse available produce.", "/profile"}}
	}
	var n []DashboardNotification
	if data.ProduceInStorage == 0 {
		n = append(n, DashboardNotification{"warning", "Add produce to storage", "Create a crop record to start tracking stock.", "/storage"})
	}
	if data.AIDiagnosesThisMonth > 0 {
		n = append(n, DashboardNotification{"success", "AI diagnosis completed", "Review your latest produce health analysis.", "/ai-diagnosis-history"})
	}
	return n
}
