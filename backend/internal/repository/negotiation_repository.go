package repository

import (
	"backend/internal/models"
	"database/sql"
)

type NegotiationRepository struct {
	db *sql.DB
}

func NewNegotiationRepository(db *sql.DB) *NegotiationRepository {
	return &NegotiationRepository{db: db}
}

func (r *NegotiationRepository) Create(n *models.Negotiation) error {
	query := `
	INSERT INTO negotiations (crop_id, buyer_id, farmer_id, quantity, status, round_count, max_rounds, expires_at)
	VALUES ($1, $2, $3, $4, $5, 0, $6, $7)
RETURNING id
	`
	err := r.db.QueryRow(
		query,
		n.CropID,
		n.BuyerID,
		n.FarmerID,
		n.Quantity,
		n.Status,
		n.MaxRounds,
		n.ExpiresAt,
	).Scan(&n.ID)

	return err
}

func (r *NegotiationRepository) IncrementRound(id int) error {
	_, err := r.db.Exec(`UPDATE negotiations SET round_count = round_count + 1 WHERE id = $1`, id)
	return err
}

func (r *NegotiationRepository) UpdateStatus(id int, status string) error {
	_, err := r.db.Exec(`UPDATE negotiations SET status = $1 WHERE id = $2`, status, id)
	return err
}

func (r *NegotiationRepository) GetByID(id int) (*models.Negotiation, error) {
	query := `
	SELECT negotiations.id, negotiations.crop_id, negotiations.buyer_id, negotiations.farmer_id,
	       negotiations.quantity, negotiations.status, negotiations.round_count, negotiations.max_rounds,
	       negotiations.created_at, negotiations.expires_at,
	       crops.name, buyer.full_name, seller.full_name, buyer.photo_url, seller.photo_url
	FROM negotiations
	JOIN crops ON crops.id = negotiations.crop_id
	JOIN farmers buyer ON buyer.id = negotiations.buyer_id
	JOIN farmers seller ON seller.id = negotiations.farmer_id
	WHERE negotiations.id = $1
	`
	var n models.Negotiation
	err := r.db.QueryRow(query, id).Scan(
		&n.ID, &n.CropID, &n.BuyerID, &n.FarmerID, &n.Quantity, &n.Status, &n.RoundCount, &n.MaxRounds,
		&n.CreatedAt, &n.ExpiresAt, &n.CropName, &n.BuyerName, &n.SellerName, &n.BuyerPhotoURL, &n.SellerPhotoURL,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &n, nil
}

// ListForUser returns every negotiation a user is part of, as buyer or seller.
func (r *NegotiationRepository) ListForUser(userID int) ([]models.Negotiation, error) {
	query := `
	SELECT negotiations.id, negotiations.crop_id, negotiations.buyer_id, negotiations.farmer_id,
	       negotiations.quantity, negotiations.status, negotiations.round_count, negotiations.max_rounds,
	       negotiations.created_at, negotiations.expires_at,
	       crops.name, buyer.full_name, seller.full_name, buyer.photo_url, seller.photo_url
	FROM negotiations
	JOIN crops ON crops.id = negotiations.crop_id
	JOIN farmers buyer ON buyer.id = negotiations.buyer_id
	JOIN farmers seller ON seller.id = negotiations.farmer_id
	WHERE negotiations.buyer_id = $1 OR negotiations.farmer_id = $2
	ORDER BY negotiations.created_at DESC
	`
	rows, err := r.db.Query(query, userID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var negotiations []models.Negotiation
	for rows.Next() {
		var n models.Negotiation
		if err := rows.Scan(
			&n.ID, &n.CropID, &n.BuyerID, &n.FarmerID, &n.Quantity, &n.Status, &n.RoundCount, &n.MaxRounds,
			&n.CreatedAt, &n.ExpiresAt, &n.CropName, &n.BuyerName, &n.SellerName, &n.BuyerPhotoURL, &n.SellerPhotoURL,
		); err != nil {
			return nil, err
		}
		negotiations = append(negotiations, n)
	}
	return negotiations, rows.Err()
}
