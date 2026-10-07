package repository

import (
	"backend/internal/models"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

func (r *MarketEventRepository) DemandSummary(produceType string, since, previousSince time.Time) (models.ProduceDemandSummary, error) {
	var s models.ProduceDemandSummary
	var currentTotal, previousTotal int
	s.ProduceType = produceType
	err := r.db.QueryRow(`
		SELECT COALESCE(SUM(CASE WHEN e.event_type='listing_view' AND e.created_at >= $2 THEN 1 ELSE 0 END),0),
		       COALESCE(SUM(CASE WHEN e.event_type='cart_item_added' AND e.created_at >= $2 THEN 1 ELSE 0 END),0),
		       COALESCE(SUM(CASE WHEN e.event_type IN ('negotiation_started','negotiation_created') AND e.created_at >= $2 THEN 1 ELSE 0 END),0),
		       COALESCE(SUM(CASE WHEN e.event_type='checkout_completed' AND e.created_at >= $2 THEN 1 ELSE 0 END),0),
		       COALESCE(SUM(CASE WHEN e.event_type='checkout_completed' AND e.created_at >= $2 THEN COALESCE((e.metadata->>'quantity')::double precision,0) ELSE 0 END),0),
		       COALESCE(SUM(CASE WHEN e.event_type='cart_item_added' AND e.created_at >= $2 THEN COALESCE((e.metadata->>'quantity')::double precision,0) ELSE 0 END),0),
		       (SELECT COUNT(*) FROM crops WHERE LOWER(name)=LOWER($1) AND listed_for_sale=true)
		FROM market_events e JOIN crops c ON c.id=e.crop_id
		WHERE LOWER(c.name)=LOWER($1) AND e.created_at >= $3`, produceType, since, previousSince).
		Scan(&s.Views, &s.CartAdds, &s.Negotiations, &s.Orders, &s.UnitsOrdered, &s.UnitsAdded, &s.CurrentListings)
	if err != nil {
		return s, err
	}
	if err := r.db.QueryRow(`SELECT COUNT(*) FILTER (WHERE e.created_at >= $2), COUNT(*) FILTER (WHERE e.created_at >= $3 AND e.created_at < $2) FROM market_events e JOIN crops c ON c.id=e.crop_id WHERE LOWER(c.name)=LOWER($1)`, produceType, since, previousSince).Scan(&currentTotal, &previousTotal); err != nil {
		return s, err
	}
	s.TrendPercent = calculateTrendPercent(currentTotal, previousTotal)
	total := s.Views + s.CartAdds + s.Negotiations + s.Orders
	switch {
	case total < 3:
		s.Activity = "Not enough data"
	case total < 10:
		s.Activity = "Early signal"
	case total < 25:
		s.Activity = "Moderate activity"
	default:
		s.Activity = "Strong activity"
	}
	return s, nil
}

func (r *MarketEventRepository) GroupedDemandSummaries(farmerID int, location string, since, previous time.Time) ([]models.ProduceDemandSummary, error) {
	rows, err := r.db.Query(`SELECT DISTINCT name FROM crops WHERE farmer_id=$1 ORDER BY name`, farmerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	result := make([]models.ProduceDemandSummary, 0, len(names))
	for _, name := range names {
		s, err := r.DemandSummary(name, since, previous)
		if err != nil {
			return nil, err
		}
		s.ComparisonLocation = location
		var minimum, median, maximum sql.NullFloat64
		var count int
		if err := r.db.QueryRow(`SELECT MIN(price_per_unit), PERCENTILE_CONT(0.5) WITHIN GROUP (ORDER BY price_per_unit), MAX(price_per_unit), COUNT(*) FROM crops WHERE LOWER(name)=LOWER($1) AND listed_for_sale=true AND LOWER(location)=LOWER($2) AND farmer_id<>$3`, name, location, farmerID).Scan(&minimum, &median, &maximum, &count); err != nil {
			return nil, err
		} else if count > 0 {
			s.LocalPriceMin, s.LocalPriceMedian, s.LocalPriceMax = minimum.Float64, median.Float64, maximum.Float64
			s.PriceAvailable = true
		}
		if err := r.db.QueryRow(`SELECT COUNT(*), COALESCE(PERCENTILE_CONT(0.5) WITHIN GROUP (ORDER BY price_per_unit),0) FROM crops WHERE listed_for_sale=true AND LOWER(name)=LOWER($1) AND farmer_id<>$2 AND LOWER(state)=LOWER((SELECT state FROM farmers WHERE id=$2))`, name, farmerID).Scan(&s.StateListings, &s.StatePriceMedian); err != nil {
			return nil, err
		}
		if err := r.db.QueryRow(`SELECT COUNT(*), COALESCE(PERCENTILE_CONT(0.5) WITHIN GROUP (ORDER BY price_per_unit),0) FROM crops WHERE listed_for_sale=true AND LOWER(name)=LOWER($1) AND farmer_id<>$2 AND LOWER(lga) IN (SELECT LOWER(neighbor_lga) FROM lga_neighbors WHERE LOWER(state)=LOWER((SELECT state FROM farmers WHERE id=$2)) AND LOWER(lga)=LOWER((SELECT lga FROM farmers WHERE id=$2)))`, name, farmerID).Scan(&s.NearbyListings, &s.NearbyPriceMedian); err != nil {
			return nil, err
		}
		if err := r.db.QueryRow(`SELECT COUNT(*), COALESCE(PERCENTILE_CONT(0.5) WITHIN GROUP (ORDER BY price_per_unit),0) FROM crops WHERE listed_for_sale=true AND LOWER(name)=LOWER($1) AND farmer_id<>$2`, name, farmerID).Scan(&s.NationalListings, &s.NationalPriceMedian); err != nil {
			return nil, err
		}
		s.ComparisonScope = "Local LGA"
		if s.StateListings == 0 && s.NationalListings == 0 {
			s.ComparisonScope = "No independent listings"
		}
		if err := r.db.QueryRow(`SELECT COALESCE(AVG(price_per_unit),0) FROM crops WHERE farmer_id=$1 AND LOWER(name)=LOWER($2) AND listed_for_sale=true`, farmerID, name).Scan(&s.FarmerPrice); err != nil {
			return nil, err
		}
		if err := r.db.QueryRow(`SELECT COUNT(*), COALESCE(PERCENTILE_CONT(0.5) WITHIN GROUP (ORDER BY price_per_unit),0) FROM external_market_prices WHERE LOWER(produce_type)=LOWER($1) AND collected_at >= $2 AND (state IS NULL OR LOWER(state)=LOWER((SELECT state FROM farmers WHERE id=$3)))`, name, since, farmerID).Scan(&s.ExternalListings, &s.ExternalPriceMedian); err != nil {
			return nil, err
		}
		s.ExternalPriceAvailable = s.ExternalListings > 0 && s.ExternalPriceMedian > 0
		if s.PriceAvailable && s.LocalPriceMedian > 0 && s.FarmerPrice > 0 {
			s.PriceDifferencePercent = (s.FarmerPrice - s.LocalPriceMedian) * 100 / s.LocalPriceMedian
			if s.PriceDifferencePercent > 5 {
				s.PricePosition = "Above local median"
			} else if s.PriceDifferencePercent < -5 {
				s.PricePosition = "Below local median"
			} else {
				s.PricePosition = "Near local median"
			}
		}
		if s.ComparisonScope == "No independent listings" && s.Views+s.CartAdds+s.Orders+s.Negotiations == 0 {
			s.Recommendation = "No buyer activity yet. Add a clear photo and keep availability current."
		} else {
			s.Recommendation = demandRecommendation(s)
		}
		result = append(result, s)
	}
	return result, nil
}

func (r *MarketEventRepository) FarmerDemandSummary(farmerID int, location string, since, previousSince time.Time) (models.ProduceDemandSummary, error) {
	var s models.ProduceDemandSummary
	var current, previous int
	err := r.db.QueryRow(`SELECT COUNT(*) FILTER (WHERE e.created_at >= $2), COUNT(*) FILTER (WHERE e.created_at >= $3 AND e.created_at < $2), COUNT(*) FILTER (WHERE e.event_type='listing_view' AND e.created_at >= $2), COUNT(*) FILTER (WHERE e.event_type='cart_item_added' AND e.created_at >= $2), COUNT(*) FILTER (WHERE e.event_type IN ('negotiation_started','negotiation_created') AND e.created_at >= $2), COUNT(*) FILTER (WHERE e.event_type='checkout_completed' AND e.created_at >= $2), COALESCE(SUM(CASE WHEN e.event_type='checkout_completed' AND e.created_at >= $2 THEN COALESCE((e.metadata->>'quantity')::double precision,0) ELSE 0 END),0), COUNT(DISTINCT c.id) FROM market_events e JOIN crops c ON c.id=e.crop_id WHERE c.farmer_id=$1`, farmerID, since, previousSince).Scan(&current, &previous, &s.Views, &s.CartAdds, &s.Negotiations, &s.Orders, &s.UnitsOrdered, &s.CurrentListings)
	_ = r.db.QueryRow(`SELECT COUNT(*) FROM marketplace_searches WHERE created_at >= $1`, since).Scan(&s.Searches)
	_ = r.db.QueryRow(`SELECT COUNT(*) FROM marketplace_searches WHERE created_at >= $1 AND LOWER(location)=LOWER($2)`, since, location).Scan(&s.Searches)
	_ = r.db.QueryRow(`SELECT COUNT(*) FROM crops WHERE listed_for_sale=true AND LOWER(location)=LOWER($1)`, location).Scan(&s.LocalListings)
	s.ComparisonLocation = location
	_ = r.db.QueryRow(`SELECT COUNT(*) FILTER (WHERE status='open'), COUNT(*) FILTER (WHERE status='accepted'), COUNT(*) FILTER (WHERE status='rejected'), COALESCE(SUM(quantity),0) FROM negotiations WHERE farmer_id=$1 AND created_at >= $2`, farmerID, since).Scan(&s.NegotiationsActive, &s.NegotiationsAccepted, &s.NegotiationsRejected, &s.NegotiatedUnits)
	_ = r.db.QueryRow(`SELECT COALESCE(AVG(m.offer_price),0) FROM negotiation_messages m JOIN negotiations n ON n.id=m.negotiation_id WHERE n.farmer_id=$1 AND m.message_type='offer' AND m.offer_price>0 AND m.created_at >= $2`, farmerID, since).Scan(&s.AverageOfferPrice)
	var initial, remaining float64
	_ = r.db.QueryRow(`SELECT COALESCE(SUM(initial_listed_quantity),0), COALESCE(SUM(quantity),0), COALESCE(AVG(EXTRACT(EPOCH FROM (first_order_at-first_listed_at))/3600) FILTER (WHERE first_order_at IS NOT NULL AND first_listed_at IS NOT NULL),0), COALESCE(AVG(EXTRACT(EPOCH FROM (sold_out_at-first_listed_at))/3600) FILTER (WHERE sold_out_at IS NOT NULL AND first_listed_at IS NOT NULL),0) FROM crops WHERE farmer_id=$1`, farmerID).Scan(&initial, &remaining, &s.TimeToFirstOrderHours, &s.TimeToSellOutHours)
	s.SellThroughRate = calculateSellThrough(initial, remaining)
	s.LifecycleDataAvailable = initial > 0
	if s.NegotiationsActive+s.NegotiationsAccepted+s.NegotiationsRejected > 0 {
		s.NegotiationConversionRate = float64(s.NegotiationsAccepted) * 100 / float64(s.NegotiationsActive+s.NegotiationsAccepted+s.NegotiationsRejected)
	}
	s.DemandScore = calculateDemandScore(s.Views, s.CartAdds, s.Orders, s.NegotiationsActive+s.NegotiationsAccepted+s.NegotiationsRejected)
	if err != nil {
		return s, err
	}
	s.ProduceType = "Your produce"
	s.TrendPercent = calculateTrendPercent(current, previous)
	s.Activity = classifyActivity(current)
	return s, nil
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func classifyActivity(total int) string {
	switch {
	case total < 3:
		return "Not enough data"
	case total < 10:
		return "Early signal"
	case total < 25:
		return "Moderate activity"
	default:
		return "Strong activity"
	}
}

func calculateTrendPercent(current, previous int) float64 {
	if previous <= 0 {
		return 0
	}
	return float64(current-previous) * 100 / float64(previous)
}

func calculateDemandScore(views, cartAdds, orders, negotiations int) float64 {
	return minFloat(float64(views)/20*100, 100)*.20 + minFloat(float64(cartAdds)/10*100, 100)*.25 + minFloat(float64(orders)/5*100, 100)*.35 + minFloat(float64(negotiations)/10*100, 100)*.20
}

func calculateSellThrough(initial, remaining float64) float64 {
	if initial <= 0 {
		return 0
	}
	rate := (initial - remaining) * 100 / initial
	if rate < 0 {
		return 0
	}
	if rate > 100 {
		return 100
	}
	return rate
}

func demandRecommendation(s models.ProduceDemandSummary) string {
	if s.PriceDifferencePercent > 15 && s.Orders == 0 {
		return fmt.Sprintf("Your price is %.0f%% above the local median. Consider reviewing it.", s.PriceDifferencePercent)
	}
	if s.Views+s.CartAdds+s.Orders+s.Negotiations == 0 {
		return "No recent activity. Review the listing price, photo, and availability."
	}
	if s.CartAdds > 0 && s.Orders == 0 {
		return "Buyers are showing intent but not completing orders. Review price and delivery terms."
	}
	if s.Orders > 0 {
		return "Demand is converting into orders. Keep quantity and availability current."
	}
	return "Monitor activity and keep listing details current."
}

type MarketEventRepository struct{ db *sql.DB }

func NewMarketEventRepository(db *sql.DB) *MarketEventRepository {
	return &MarketEventRepository{db: db}
}

// RecordListingView records at most one view for a user/session and crop
// within the supplied deduplication window.
func (r *MarketEventRepository) RecordListingView(cropID int, userID *int, sessionID string, window time.Duration) error {
	if cropID <= 0 {
		return fmt.Errorf("crop id is required")
	}
	if window <= 0 {
		window = 30 * time.Minute
	}
	_, err := r.db.Exec(`
		INSERT INTO market_events (event_type, crop_id, user_id, session_id, metadata)
		SELECT 'listing_view', $1, $2, NULLIF($3, ''), '{}'::jsonb
		WHERE NOT EXISTS (
			SELECT 1 FROM market_events
			WHERE event_type = 'listing_view' AND crop_id = $1
			  AND created_at >= CURRENT_TIMESTAMP - $4::interval
			  AND ((user_id IS NOT NULL AND user_id = $2) OR (session_id IS NOT NULL AND session_id = NULLIF($3, '')))
		)`, cropID, userID, sessionID, fmt.Sprintf("%f seconds", window.Seconds()))
	return err
}

func (r *MarketEventRepository) Record(event *models.MarketEvent) error {
	if event == nil {
		return fmt.Errorf("market event is nil")
	}
	metadata := strings.TrimSpace(event.Metadata)
	if metadata == "" {
		metadata = "{}"
	}
	_, err := r.db.Exec(`INSERT INTO market_events (event_type, crop_id, user_id, session_id, metadata) VALUES ($1, $2, $3, $4, $5)`, event.EventType, event.CropID, event.UserID, event.SessionID, metadata)
	return err
}

// RecentForFarmer returns recent non-order, non-listing market events
// relevant to a farmer's dashboard activity feed: crop updates, AI
// diagnoses, delivered orders, and negotiations started on their produce.
// Order and listing activity are derived elsewhere from the orders/crops
// tables directly, so they're deliberately excluded here to avoid duplicates.
func (r *MarketEventRepository) RecentForFarmer(farmerID int, limit int) ([]models.MarketEvent, error) {
	if farmerID <= 0 {
		return nil, fmt.Errorf("invalid farmer id")
	}
	if limit <= 0 {
		limit = 5
	}

	rows, err := r.db.Query(`
		SELECT
			e.id, e.event_type, e.crop_id, e.user_id,
			COALESCE(e.session_id, ''), e.metadata, e.created_at,
			COALESCE(c.name, '')
		FROM market_events e
		LEFT JOIN crops c ON c.id = e.crop_id
		WHERE e.event_type IN ('crop_updated', 'diagnosis_completed', 'delivery_delivered', 'negotiation_started')
		AND ((c.farmer_id = $1) OR (e.crop_id IS NULL AND e.user_id = $1))
		ORDER BY e.created_at DESC
		LIMIT $2
	`, farmerID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []models.MarketEvent
	for rows.Next() {
		var ev models.MarketEvent
		if err := rows.Scan(
			&ev.ID, &ev.EventType, &ev.CropID, &ev.UserID,
			&ev.SessionID, &ev.Metadata, &ev.CreatedAt,
			&ev.CropName,
		); err != nil {
			return nil, err
		}
		events = append(events, ev)
	}
	return events, rows.Err()
}

func (r *MarketEventRepository) RecordSearch(term, normalized, location string, userID *int, sessionID string) error {
	term = strings.TrimSpace(term)
	if term == "" {
		return nil
	}
	if strings.TrimSpace(normalized) == "" {
		normalized = term
	}
	_, err := r.db.Exec(`INSERT INTO marketplace_searches (search_term, normalized_produce, location, user_id, session_id) VALUES ($1, NULLIF($2,''), NULLIF($3,''), $4, NULLIF($5,''))`, term, strings.ToLower(strings.TrimSpace(normalized)), strings.TrimSpace(location), userID, sessionID)
	return err
}

func (r *MarketEventRepository) RecordPrice(cropID int, price float64, unit, source string) error {
	if cropID <= 0 || price <= 0 || strings.TrimSpace(unit) == "" {
		return fmt.Errorf("invalid price history record")
	}
	if strings.TrimSpace(source) == "" {
		source = "Agro-Shield"
	}
	_, err := r.db.Exec(`INSERT INTO produce_price_history (crop_id,price_per_unit,unit,source) VALUES ($1,$2,$3,$4)`, cropID, price, unit, source)
	return err
}

// RecordExternalPrice stores a verified external observation with provenance.
func (r *MarketEventRepository) RecordExternalPrice(produce string, price float64, unit, provider, market, lga, state, country, sourceURL string, collectedAt time.Time) error {
	if err := validateExternalPrice(produce, price, unit, provider, market, collectedAt); err != nil {
		return err
	}
	country = strings.TrimSpace(country)
	if country == "" {
		return fmt.Errorf("country is required")
	}
	_, err := r.db.Exec(`INSERT INTO external_market_prices (produce_type,price_per_unit,unit,provider,market_name,lga,state,country,collected_at,source_url) VALUES ($1,$2,$3,$4,$5,NULLIF($6,''),NULLIF($7,''),$8,$9,NULLIF($10,''))`, strings.TrimSpace(produce), price, unit, provider, market, strings.TrimSpace(lga), strings.TrimSpace(state), country, collectedAt, strings.TrimSpace(sourceURL))
	return err
}

// UpsertLGANeighbor stores a directional LGA relationship. Call it for both
// directions when the source data defines mutual adjacency.
func (r *MarketEventRepository) UpsertLGANeighbor(state, lga, neighbor string) error {
	state, lga, neighbor = strings.TrimSpace(state), strings.TrimSpace(lga), strings.TrimSpace(neighbor)
	if state == "" || lga == "" || neighbor == "" || strings.EqualFold(lga, neighbor) {
		return fmt.Errorf("invalid LGA neighbor relationship")
	}
	_, err := r.db.Exec(`INSERT INTO lga_neighbors (state,lga,neighbor_lga) VALUES ($1,$2,$3) ON CONFLICT (state,lga,neighbor_lga) DO NOTHING`, state, lga, neighbor)
	return err
}

func validateExternalPrice(produce string, price float64, unit, provider, market string, collectedAt time.Time) error {
	if strings.TrimSpace(produce) == "" || price <= 0 || strings.TrimSpace(unit) == "" || strings.TrimSpace(provider) == "" || strings.TrimSpace(market) == "" || collectedAt.IsZero() {
		return fmt.Errorf("incomplete external price observation")
	}
	return nil
}

// ReconcileCheckoutAbandonments records one abandonment for checkout starts
// that remained incomplete beyond the timeout. It is safe to run repeatedly.
func (r *MarketEventRepository) ReconcileCheckoutAbandonments(timeout time.Duration) error {
	if timeout <= 0 {
		timeout = 24 * time.Hour
	}
	_, err := r.db.Exec(`INSERT INTO market_events (event_type,user_id,session_id,metadata,created_at)
		SELECT 'checkout_abandoned', s.user_id, s.session_id, jsonb_build_object('checkout_started_event_id',s.id), CURRENT_TIMESTAMP
		FROM market_events s
		WHERE s.event_type='checkout_started' AND s.created_at < CURRENT_TIMESTAMP-$1::interval
		AND NOT EXISTS (SELECT 1 FROM market_events c WHERE c.event_type='checkout_completed' AND c.created_at>=s.created_at AND ((s.user_id IS NOT NULL AND c.user_id=s.user_id) OR (s.session_id IS NOT NULL AND c.session_id=s.session_id)))
		AND NOT EXISTS (SELECT 1 FROM market_events a WHERE a.event_type='checkout_abandoned' AND a.metadata->>'checkout_started_event_id'=s.id::text)`, fmt.Sprintf("%f seconds", timeout.Seconds()))
	return err
}
