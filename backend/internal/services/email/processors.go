package emailservice

import "fmt"

type MessageSkipError struct {
	Code  string
	Label string
}

func (e *MessageSkipError) Error() string {
	if e == nil {
		return ""
	}
	if e.Label != "" {
		return e.Label
	}
	if e.Code != "" {
		return e.Code
	}
	return "message skipped"
}

func SkipMessage(code, label string) error {
	if code == "" {
		code = "skipped"
	}
	if label == "" {
		label = fmt.Sprintf("ไม่สร้างบิลใหม่ (%s)", code)
	}
	return &MessageSkipError{Code: code, Label: label}
}

// Processors bundles the three downstream message handlers that the
// coordinator dispatches to based on each account's channel + the
// message's subject. One bundle is shared by all account pollers.
type Processors struct {
	// Attachment is the generic PDF/image/Excel pipeline used by
	// channel="general" and channel="lazada" (until the dedicated
	// Lazada handler ships).
	Attachment AttachmentProcessor

	// ShopeeOrder handles Shopee email order confirmations (saleinvoice
	// flow). Used for channel="shopee" when the subject does NOT contain
	// "ถูกจัดส่งแล้ว".
	ShopeeOrder ShopeeBodyProcessor

	// ShopeeShipped handles Shopee email shipping confirmations
	// (purchaseorder flow). Used for channel="shopee" when the subject
	// contains "ถูกจัดส่งแล้ว".
	ShopeeShipped ShopeeBodyProcessor

	// DuplicateMessage returns true when a Message-ID has already been
	// processed. It lets the poller avoid fetching body/AI work for old
	// read+unread messages while still reporting a user-friendly summary.
	DuplicateMessage func(messageID string) (bool, error)

	// DuplicateMessages is the batch form used by IMAP envelope polling so a
	// mailbox with hundreds of old read+unread messages does one DB lookup per
	// batch instead of one lookup per message.
	DuplicateMessages func(messageIDs []string) (map[string]bool, error)
}
