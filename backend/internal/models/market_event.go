package models

import "time"

// MarketEvent records buyer activity used for transparent demand signals.
type MarketEvent struct {
	ID        int
	EventType string
	CropID    *int
	UserID    *int
	SessionID string
	Metadata  string
	CreatedAt time.Time
}

type ProduceDemandSummary struct {
	ProduceType                                                    string
	Views, CartAdds, Negotiations, Orders                          int
	UnitsOrdered, UnitsAdded                                       float64
	CurrentListings                                                int
	Activity                                                       string
	TrendPercent                                                   float64
	Searches                                                       int
	NegotiationsActive, NegotiationsAccepted, NegotiationsRejected int
	NegotiatedUnits, AverageOfferPrice, NegotiationConversionRate  float64
	DemandScore                                                    float64
	ComparisonLocation                                             string
	LocalListings                                                  int
	SellThroughRate, TimeToFirstOrderHours, TimeToSellOutHours     float64
	LifecycleDataAvailable                                         bool
	LocalPriceMin, LocalPriceMedian, LocalPriceMax                 float64
	PriceAvailable                                                 bool
	Recommendation                                                 string
	FarmerPrice, PriceDifferencePercent                            float64
	PricePosition                                                  string
	StateListings, NationalListings                                int
	NearbyListings                                                 int
	StatePriceMedian, NationalPriceMedian                          float64
	NearbyPriceMedian                                              float64
	ExternalListings                                               int
	ExternalPriceMedian                                            float64
	ExternalPriceAvailable                                         bool
	ComparisonScope                                                string
}
