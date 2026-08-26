package ussd

import (
	"context"
	"strings"
	"testing"

	"backend/internal/models"
	"backend/internal/services"
)

type fakeFarmers struct{ farmer *models.Farmer }

func (f fakeFarmers) GetByPhone(string) (*models.Farmer, error) { return f.farmer, nil }

type fakeCrops struct{ crops []models.Crop }

func (f fakeCrops) AvailableCrops() ([]models.Crop, error) { return f.crops, nil }
func (f fakeCrops) MyCrops(int) ([]models.Crop, error)     { return f.crops, nil }

type fakeWeather struct{}

func (fakeWeather) Current(context.Context, string) (services.Weather, error) {
	return services.Weather{Available: true, Location: "Makurdi", Temperature: "30C", Summary: "Clear", RainChance: "10%", Recommendation: "Water crops early."}, nil
}

func testHandler() *Handler {
	return &Handler{farmers: fakeFarmers{&models.Farmer{FullName: "Amina", Phone: "+2341", Location: "Makurdi"}}, crops: fakeCrops{[]models.Crop{{Name: "Maize", PricePerUnit: 500, Unit: "kg"}}}, weather: fakeWeather{}}
}

func TestMainMenuContinuesSession(t *testing.T) {
	got := testHandler().respond(context.Background(), "+2341", "")
	if !strings.HasPrefix(got, "CON Agro-Shield") {
		t.Fatalf("got %q", got)
	}
}

func TestStorageEndsSession(t *testing.T) {
	got := testHandler().respond(context.Background(), "+2341", "1")
	if !strings.HasPrefix(got, "END Storage:") {
		t.Fatalf("got %q", got)
	}
}

func TestMarketPricesEndSession(t *testing.T) {
	got := testHandler().respond(context.Background(), "+2341", "2")
	if got != "END Marketplace:\n0. Maize ₦500.00/kg" {
		t.Fatalf("got %q", got)
	}
}

func TestUnknownCallerCannotViewProfile(t *testing.T) {
	h := testHandler()
	h.farmers = fakeFarmers{}
	got := h.respond(context.Background(), "+000", "3")
	if !strings.HasPrefix(got, "END This number is not registered") {
		t.Fatalf("got %q", got)
	}
}

func TestUnknownCallerIsRejectedBeforeMenu(t *testing.T) {
	h := testHandler()
	h.farmers = fakeFarmers{}
	got := h.respond(context.Background(), "+000", "")
	if !strings.HasPrefix(got, "END This number is not registered") {
		t.Fatalf("got %q", got)
	}
}
