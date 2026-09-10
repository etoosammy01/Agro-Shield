package repository

import (
	"backend/internal/models"
	"database/sql"
)

type FeedbackRepository struct {
	db *sql.DB
}

func NewFeedbackRepository(db *sql.DB) *FeedbackRepository {

	return &FeedbackRepository{
		db: db,
	}
}

func (r *FeedbackRepository) Create(feedback *models.Feedback) error {

	query := `
	INSERT INTO feedback
	(user_id, farmer_id, product_id, rating, comment)
	VALUES ($1,$2,$3,$4,$5)
	`

	_, err := r.db.Exec(
		query,
		feedback.UserID,
		feedback.FarmerID,
		feedback.ProductID,
		feedback.Rating,
		feedback.Comment,
	)

	return err
}
