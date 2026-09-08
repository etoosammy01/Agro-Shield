package services

import (
	"errors"
	"fmt"
	"time"

	"backend/internal/models"
	"backend/internal/repository"
)

const (
	MaxNegotiationRounds = 15
	NegotiationWindow    = 24 * time.Hour
)

// ============================================================
// NEGOTIATION SERVICE
//
// Responsibility:
// - Start negotiations.
// - Send normal chat messages.
// - Send offers/counter-offers.
// - Accept individual offers.
// - Reject individual offers.
// - Keep rejected offers inside the conversation.
// - Keep chat history.
// - Move accepted deals into the buyer's cart.
// - Send negotiation notifications.
//
// IMPORTANT:
//
// Negotiation chat and negotiation offers are treated
// differently.
//
// CHAT:
// - Unlimited.
// - Does not consume rounds.
// - Not affected by the 24-hour offer window.
//
// OFFERS:
// - Limited to MaxNegotiationRounds.
// - Affected by NegotiationWindow.
// - Can be accepted or rejected.
// ============================================================

type NegotiationService struct {
	repo         *repository.NegotiationRepository
	msgRepo      *repository.NegotiationMessageRepository
	cropRepo     *repository.CropRepository
	cartService  *CartService
	notification *NotificationService
	eventRepo    *repository.MarketEventRepository
}

// ============================================================
// CREATE NEGOTIATION SERVICE
// ============================================================

func NewNegotiationService(
	repo *repository.NegotiationRepository,
	msgRepo *repository.NegotiationMessageRepository,
	cropRepo *repository.CropRepository,
	cartService *CartService,
	notification *NotificationService,
	eventRepo *repository.MarketEventRepository,
) *NegotiationService {

	return &NegotiationService{
		repo:         repo,
		msgRepo:      msgRepo,
		cropRepo:     cropRepo,
		cartService:  cartService,
		notification: notification,
		eventRepo:    eventRepo,
	}
}

// ============================================================
// START NEGOTIATION
//
// Creates a new negotiation.
//
// The first message is stored as an OFFER.
//
// The 24-hour negotiation window starts here.
// ============================================================

func (s *NegotiationService) StartNegotiation(
	buyerID,
	cropID int,
	quantity,
	offerPrice float64,
	message string,
) (*models.Negotiation, error) {

	crop, err := s.cropRepo.GetByID(cropID)
	if err != nil {
		return nil, err
	}

	if crop == nil || !crop.ListedForSale {
		return nil, errors.New(
			"this produce is not available for negotiation",
		)
	}

	if crop.FarmerID == buyerID {
		return nil, errors.New(
			"you can't negotiate on your own produce",
		)
	}

	if quantity <= 0 {
		return nil, errors.New(
			"quantity must be greater than zero",
		)
	}

	if quantity > crop.Quantity {
		return nil, errors.New(
			"not enough quantity available",
		)
	}

	if offerPrice <= 0 {
		return nil, errors.New(
			"offer price must be greater than zero",
		)
	}

	negotiation := &models.Negotiation{
		CropID:    cropID,
		BuyerID:   buyerID,
		FarmerID:  crop.FarmerID,
		Quantity:  quantity,
		Status:    "open",
		MaxRounds: MaxNegotiationRounds,
		ExpiresAt: time.Now().Add(NegotiationWindow),
	}

	if err := s.repo.Create(negotiation); err != nil {
		return nil, err
	}

	// ========================================================
	// FIRST OFFER
	//
	// The first message starts the negotiation.
	// ========================================================

	firstOffer := &models.NegotiationMessage{
		NegotiationID: negotiation.ID,
		SenderID:      buyerID,
		MessageType:   "offer",
		OfferPrice:    offerPrice,
		Message:       message,
		OfferStatus:   "pending",
	}

	if err := s.msgRepo.Create(firstOffer); err != nil {
		return nil, err
	}

	// First offer uses one round.
	if err := s.repo.IncrementRound(negotiation.ID); err != nil {
		return nil, err
	}

	// ========================================================
	// NOTIFICATION
	// ========================================================

	if s.notification != nil {
		_ = s.notification.CreateNotification(
			crop.FarmerID,
			"New negotiation",
			"A buyer has started a negotiation for your produce.",
			"negotiation",
		)
	}

	// Record this as an activity event for the farmer's dashboard.
	if s.eventRepo != nil {
		cid := cropID
		uid := buyerID
		_ = s.eventRepo.Record(&models.MarketEvent{
			EventType: "negotiation_started",
			CropID:    &cid,
			UserID:    &uid,
			Metadata:  fmt.Sprintf(`{"quantity":%g}`, quantity),
		})
	}

	return negotiation, nil
}

// ============================================================
// SEND CHAT MESSAGE
//
// Sends a NORMAL conversation message.
//
// IMPORTANT:
//
// Chat messages:
// - Do NOT consume negotiation rounds.
// - Do NOT have a 15-round limit.
// - Are NOT blocked when the negotiation offer window expires.
// - Remain visible in the conversation history.
//
// The 24-hour timer applies to OFFERS only.
// ============================================================

func (s *NegotiationService) SendMessage(
	negotiationID,
	senderID int,
	message string,
) error {

	negotiation, err := s.repo.GetByID(negotiationID)
	if err != nil {
		return err
	}

	if negotiation == nil {
		return errors.New(
			"negotiation not found",
		)
	}

	// --------------------------------------------------------
	// Make sure the sender belongs to this negotiation.
	// --------------------------------------------------------

	if senderID != negotiation.BuyerID &&
		senderID != negotiation.FarmerID {

		return errors.New(
			"you're not part of this negotiation",
		)
	}

	// --------------------------------------------------------
	// Message cannot be empty.
	// --------------------------------------------------------

	if message == "" {
		return errors.New(
			"message cannot be empty",
		)
	}

	// --------------------------------------------------------
	// Create normal chat message.
	// --------------------------------------------------------

	chatMessage := &models.NegotiationMessage{
		NegotiationID: negotiationID,
		SenderID:      senderID,
		MessageType:   "chat",
		OfferPrice:    0,
		Message:       message,
		OfferStatus:   "none",
	}

	if err := s.msgRepo.Create(chatMessage); err != nil {
		return err
	}

	// ========================================================
	// NOTIFY THE OTHER PARTICIPANT
	// ========================================================

	if s.notification != nil {

		recipientID := negotiation.FarmerID

		if senderID == negotiation.FarmerID {
			recipientID = negotiation.BuyerID
		}

		_ = s.notification.CreateNotification(
			recipientID,
			"New negotiation message",
			"You received a new message in a negotiation.",
			"negotiation_message",
		)
	}

	return nil
}




// ============================================================
// SEND OFFER
//
// Sends a new offer/counter-offer.
//
// IMPORTANT:
// Only OFFERS increase the negotiation round count.
//
// Normal chat messages do NOT use rounds.
//
// Offers are affected by:
// - negotiation status
// - expiration time
// - maximum rounds
// ============================================================

func (s *NegotiationService) SendOffer(
	negotiationID,
	senderID int,
	offerPrice float64,
	message string,
) error {

	negotiation, err := s.repo.GetByID(negotiationID)
	if err != nil {
		return err
	}

	if negotiation == nil {
		return errors.New(
			"negotiation not found",
		)
	}

	// --------------------------------------------------------
	// Verify participant.
	// --------------------------------------------------------

	if senderID != negotiation.BuyerID &&
		senderID != negotiation.FarmerID {

		return errors.New(
			"you're not part of this negotiation",
		)
	}

	// --------------------------------------------------------
	// Negotiation must still be open.
	// --------------------------------------------------------

	if negotiation.Status != "open" {
		return errors.New(
			"this negotiation is no longer open",
		)
	}

	// --------------------------------------------------------
	// Check expiration.
	// --------------------------------------------------------

	if negotiation.IsExpired() {

		_ = s.repo.UpdateStatus(
			negotiationID,
			"expired",
		)

		return errors.New(
			"this negotiation has expired",
		)
	}

	// --------------------------------------------------------
	// Check round limit.
	// --------------------------------------------------------

	if negotiation.RoundCount >= negotiation.MaxRounds {
		return errors.New(
			"you've reached the negotiation round limit — no more offers can be made",
		)
	}

	// --------------------------------------------------------
	// Validate offer price.
	// --------------------------------------------------------

	if offerPrice <= 0 {
		return errors.New(
			"offer price must be greater than zero",
		)
	}

	// --------------------------------------------------------
	// Create new offer.
	// --------------------------------------------------------

	newOffer := &models.NegotiationMessage{
		NegotiationID: negotiationID,
		SenderID:      senderID,
		MessageType:   "offer",
		OfferPrice:    offerPrice,
		Message:       message,
		OfferStatus:   "pending",
	}

	if err := s.msgRepo.Create(newOffer); err != nil {
		return err
	}

	// --------------------------------------------------------
	// Increase negotiation round.
	// --------------------------------------------------------

	if err := s.repo.IncrementRound(negotiationID); err != nil {
		return err
	}

	// ========================================================
	// NOTIFY OTHER PARTICIPANT
	// ========================================================

	if s.notification != nil {

		recipientID := negotiation.FarmerID

		if senderID == negotiation.FarmerID {
			recipientID = negotiation.BuyerID
		}

		_ = s.notification.CreateNotification(
			recipientID,
			"New price offer",
			"You received a new price offer.",
			"negotiation_offer",
		)
	}

	return nil
}

// ============================================================
// ACCEPT OFFER
//
// Accepts ONE specific offer.
//
// The selected offer becomes accepted.
// The negotiation becomes accepted.
// The accepted deal goes into the buyer's cart.
// ============================================================

func (s *NegotiationService) Accept(
	negotiationID,
	offerID,
	accepterID int,
) error {

	negotiation, err := s.repo.GetByID(negotiationID)
	if err != nil {
		return err
	}

	if negotiation == nil {
		return errors.New(
			"negotiation not found",
		)
	}

	// --------------------------------------------------------
	// Verify participant.
	// --------------------------------------------------------

	if accepterID != negotiation.BuyerID &&
		accepterID != negotiation.FarmerID {

		return errors.New(
			"you're not part of this negotiation",
		)
	}

	// --------------------------------------------------------
	// Negotiation must still be open.
	// --------------------------------------------------------

	if negotiation.Status != "open" {
		return errors.New(
			"this negotiation is already finalized",
		)
	}

	// --------------------------------------------------------
	// Check expiration.
	// --------------------------------------------------------

	if negotiation.IsExpired() {

		_ = s.repo.UpdateStatus(
			negotiationID,
			"expired",
		)

		return errors.New(
			"this negotiation has expired",
		)
	}

	// --------------------------------------------------------
	// Find offer.
	// --------------------------------------------------------

	offer, err := s.msgRepo.GetByID(offerID)
	if err != nil {
		return err
	}

	if offer == nil {
		return errors.New(
			"offer not found",
		)
	}

	// --------------------------------------------------------
	// Make sure offer belongs to this negotiation.
	// --------------------------------------------------------

	if offer.NegotiationID != negotiationID {
		return errors.New(
			"this offer does not belong to this negotiation",
		)
	}

	// --------------------------------------------------------
	// Only offers can be accepted.
	// --------------------------------------------------------

	if offer.MessageType != "offer" {
		return errors.New(
			"this message is not an offer",
		)
	}

	// --------------------------------------------------------
	// Only pending offers can be accepted.
	// --------------------------------------------------------

	if offer.OfferStatus != "pending" {
		return errors.New(
			"this offer is no longer pending",
		)
	}

	if offer.SenderID == accepterID {
		return errors.New("you cannot accept your own offer")
	}

	// --------------------------------------------------------
	// Get crop.
	// --------------------------------------------------------

	crop, err := s.cropRepo.GetByID(
		negotiation.CropID,
	)

	if err != nil {
		return err
	}

	if crop == nil {
		return errors.New(
			"product no longer exists",
		)
	}

	// --------------------------------------------------------
	// Make sure crop is still available.
	// --------------------------------------------------------

	if !crop.ListedForSale {
		return errors.New(
			"this product is no longer available for sale",
		)
	}

	// --------------------------------------------------------
	// Check quantity.
	// --------------------------------------------------------

	if negotiation.Quantity > crop.Quantity {
		return errors.New(
			"the requested quantity is no longer available",
		)
	}

	// --------------------------------------------------------
	// Mark offer as accepted.
	// --------------------------------------------------------

	if err := s.msgRepo.UpdateOfferStatus(
		offerID,
		"accepted",
	); err != nil {
		return err
	}

	// --------------------------------------------------------
	// Finalize negotiation.
	// --------------------------------------------------------

	if err := s.repo.UpdateStatus(
		negotiationID,
		"accepted",
	); err != nil {
		return err
	}
	if s.eventRepo != nil {
		uid := negotiation.BuyerID
		cid := negotiation.CropID
		_ = s.eventRepo.Record(&models.MarketEvent{EventType: "negotiation_accepted", CropID: &cid, UserID: &uid, Metadata: fmt.Sprintf(`{"quantity":%g}`, negotiation.Quantity)})
	}

	// --------------------------------------------------------
	// Move accepted deal into buyer's cart.
	// --------------------------------------------------------

	if err := s.cartService.AddFromNegotiation(
		negotiation,
		offer.OfferPrice,
	); err != nil {
		return err
	}

	// ========================================================
	// NOTIFY OTHER PARTICIPANT
	// ========================================================

	if s.notification != nil {

		recipientID := negotiation.BuyerID

		if accepterID == negotiation.BuyerID {
			recipientID = negotiation.FarmerID
		}

		_ = s.notification.CreateNotification(
			recipientID,
			"Negotiation accepted",
			"An offer in your negotiation has been accepted.",
			"negotiation_accepted",
		)
	}

	return nil
}

// ============================================================
// REJECT OFFER
//
// Rejects ONE specific offer.
//
// IMPORTANT:
// The negotiation remains OPEN.
//
// Example:
//
// Buyer → ₦50,000
// Seller → Reject
//
// Result:
//
// Offer = rejected
// Negotiation = still open
//
// Buyer can continue chatting or send another offer.
// ============================================================

func (s *NegotiationService) Reject(
	negotiationID,
	offerID,
	rejecterID int,
) error {

	negotiation, err := s.repo.GetByID(
		negotiationID,
	)

	if err != nil {
		return err
	}

	if negotiation == nil {
		return errors.New(
			"negotiation not found",
		)
	}

	// --------------------------------------------------------
	// Verify participant.
	// --------------------------------------------------------

	if rejecterID != negotiation.BuyerID &&
		rejecterID != negotiation.FarmerID {

		return errors.New(
			"you're not part of this negotiation",
		)
	}

	// --------------------------------------------------------
	// Negotiation must still be open.
	// --------------------------------------------------------

	if negotiation.Status != "open" {
		return errors.New(
			"this negotiation is no longer open",
		)
	}

	// --------------------------------------------------------
	// Check expiration.
	// --------------------------------------------------------

	if negotiation.IsExpired() {

		_ = s.repo.UpdateStatus(
			negotiationID,
			"expired",
		)

		return errors.New(
			"this negotiation has expired",
		)
	}

	// --------------------------------------------------------
	// Find offer.
	// --------------------------------------------------------

	offer, err := s.msgRepo.GetByID(
		offerID,
	)

	if err != nil {
		return err
	}

	if offer == nil {
		return errors.New(
			"offer not found",
		)
	}

	// --------------------------------------------------------
	// Make sure offer belongs to negotiation.
	// --------------------------------------------------------

	if offer.NegotiationID != negotiationID {
		return errors.New(
			"this offer does not belong to this negotiation",
		)
	}

	// --------------------------------------------------------
	// Only offers can be rejected.
	// --------------------------------------------------------

	if offer.MessageType != "offer" {
		return errors.New(
			"this message is not an offer",
		)
	}

	// --------------------------------------------------------
	// Only pending offers can be rejected.
	// --------------------------------------------------------

	if offer.OfferStatus != "pending" {
		return errors.New(
			"this offer is no longer pending",
		)
	}

	if offer.SenderID == rejecterID {
		return errors.New("you cannot reject your own offer")
	}

	// --------------------------------------------------------
	// Reject only this offer.
	// --------------------------------------------------------

	if err := s.msgRepo.UpdateOfferStatus(
		offerID,
		"rejected",
	); err != nil {
		return err
	}
	if s.eventRepo != nil {
		uid := negotiation.BuyerID
		cid := negotiation.CropID
		_ = s.eventRepo.Record(&models.MarketEvent{EventType: "negotiation_rejected", CropID: &cid, UserID: &uid})
	}

	// ========================================================
	// NOTIFY OTHER PARTICIPANT
	// ========================================================

	if s.notification != nil {

		recipientID := negotiation.BuyerID

		if rejecterID == negotiation.BuyerID {
			recipientID = negotiation.FarmerID
		}

		_ = s.notification.CreateNotification(
			recipientID,
			"Offer rejected",
			"An offer in your negotiation was rejected.",
			"negotiation_rejected",
		)
	}

	return nil
}

// End closes an open negotiation without accepting an offer.
func (s *NegotiationService) End(negotiationID, userID int) error {
	negotiation, err := s.repo.GetByID(negotiationID)
	if err != nil {
		return err
	}
	if negotiation == nil {
		return errors.New("negotiation not found")
	}
	if userID != negotiation.BuyerID && userID != negotiation.FarmerID {
		return errors.New("you're not part of this negotiation")
	}
	if negotiation.Status != "open" {
		return errors.New("this negotiation is already finalized")
	}
	if err := s.repo.UpdateStatus(negotiationID, "rejected"); err != nil {
		return err
	}
	if s.notification != nil {
		recipientID := negotiation.FarmerID
		if userID == negotiation.FarmerID {
			recipientID = negotiation.BuyerID
		}
		_ = s.notification.CreateNotification(recipientID, "Negotiation ended", "The other participant ended a negotiation.", "negotiation_ended")
	}
	return nil
}

// ============================================================
// THREAD
//
// Retrieves the negotiation and its COMPLETE conversation.
//
// Includes:
//
// 💬 Normal chat messages
// 💰 Offers
// ❌ Rejected offers
// ✅ Accepted offers
//
// Nothing is removed from the conversation history.
// ============================================================

func (s *NegotiationService) Thread(
	negotiationID int,
) (*models.Negotiation, []models.NegotiationMessage, error) {

	negotiation, err := s.repo.GetByID(
		negotiationID,
	)

	if err != nil || negotiation == nil {
		return negotiation, nil, err
	}

	messages, err := s.msgRepo.ListByNegotiation(
		negotiationID,
	)

	return negotiation, messages, err
}

// ============================================================
// MY NEGOTIATIONS
//
// Retrieves all negotiations involving a specific user.
//
// A user can be:
//
// - Buyer
// - Farmer/Seller
// ============================================================

func (s *NegotiationService) MyNegotiations(
	userID int,
) ([]models.Negotiation, error) {

	return s.repo.ListForUser(userID)
}