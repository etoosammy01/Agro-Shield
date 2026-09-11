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

func (r *FeedbackRepository) GetByFarmerID(farmerID int) ([]models.Feedback, error) {

	query := `
	SELECT id, user_id, farmer_id, product_id, rating, comment, created_at
	FROM feedback
	WHERE farmer_id = $1
	ORDER BY created_at DESC
	`

	rows, err := r.db.Query(query, farmerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var feedbacks []models.Feedback

	for rows.Next() {
		var feedback models.Feedback

		err := rows.Scan(
			&feedback.ID,
			&feedback.UserID,
			&feedback.FarmerID,
			&feedback.ProductID,
			&feedback.Rating,
			&feedback.Comment,
			&feedback.CreatedAt,
		)
		if err != nil {
			return nil, err
		}

		feedbacks = append(feedbacks, feedback)
	}

	return feedbacks, rows.Err()
}

func (r *FeedbackRepository) GetFarmerRating(farmerID int) (float64, int, error) {
	query := `
	SELECT COALESCE(AVG(rating), 0), COUNT(*)
	FROM feedback
	WHERE farmer_id = $1
	`

	var average float64
	var total int

	err := r.db.QueryRow(query, farmerID).Scan(&average, &total)
	if err != nil {
		return 0, 0, err
	}

	return average, total, nil
}
