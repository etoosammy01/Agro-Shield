package handlers

import (
	"backend/internal/services"
	"backend/middleware"
	"backend/render"
	"log"
	"net/http"
)

type Sales struct{ order *services.OrderService }
type SalesPageData struct {
	FarmerName string
	Sales      interface{}
	Total      float64
	Count      int
}

func NewSalesHandler(order *services.OrderService) *Sales { return &Sales{order: order} }

func (h *Sales) Handler(w http.ResponseWriter, r *http.Request) {
	farmer, ok := middleware.FarmerFromContext(r)
	if !ok || farmer == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if farmer.IsBuyer() {
		http.Redirect(w, r, "/profile", http.StatusSeeOther)
		return
	}
	sales, err := h.order.MySales(farmer.ID)
	if err != nil {
		log.Println("failed to load sales:", err)
		http.Error(w, "Unable to load sales", http.StatusInternalServerError)
		return
	}
	data := SalesPageData{FarmerName: farmer.FullName, Sales: sales, Count: len(sales)}
	for _, sale := range sales {
		data.Total += sale.TotalPrice
	}
	if err := render.RenderTemplates(w, "sales.html", data); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}
