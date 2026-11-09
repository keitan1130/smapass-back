package entity

import "time"

// Anchor states stored in anchors.status. Public APIs fold submitting / submitted
// into "pending" so that nothing before a block inclusion reads as confirmed.
const (
	AnchorPending    = "pending"    // batch created, not sent yet
	AnchorSubmitting = "submitting" // request started; outcome unknown until reconciled
	AnchorSubmitted  = "submitted"  // accepted by the provider, waiting for a block
	AnchorConfirmed  = "confirmed"  // included in a block, block info stored
	AnchorFailed     = "failed"     // rejected by the provider; needs a human decision
)

// Provider-side timestamp states (Tapyrus API).
const (
	TimestampInit        = "init"
	TimestampUnconfirmed = "unconfirmed"
	TimestampConfirmed   = "confirmed"
)

type Anchor struct {
	ID          string
	Chain       string
	MerkleRoot  [32]byte
	LeafCount   int
	Status      string
	ProviderRef *string
	RetryCount  int
}

// TimestampReceipt is what the chain provider reports about one recorded root.
type TimestampReceipt struct {
	Ref         string
	TxID        *string
	Status      string
	BlockHeight *int64
	BlockTime   *time.Time
}

// Confirmed is true only when the provider reports block inclusion with complete block data.
func (r TimestampReceipt) Confirmed() bool {
	return r.Status == TimestampConfirmed && r.TxID != nil && r.BlockHeight != nil && r.BlockTime != nil
}
