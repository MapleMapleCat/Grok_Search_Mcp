package panel

import (
	"time"
)

type InviteCodeRedemptionResponse struct {
	ID               string    `json:"id"`
	InviteCodeID     string    `json:"invite_code_id"`
	InviteCodePrefix string    `json:"invite_code_prefix"`
	UserID           string    `json:"user_id"`
	Username         string    `json:"username"`
	RedeemedAt       time.Time `json:"redeemed_at"`
}

type InviteCodeRedemptionsResponse struct {
	Redemptions []InviteCodeRedemptionResponse `json:"redemptions"`
}
