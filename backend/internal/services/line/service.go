package lineservice

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/line/line-bot-sdk-go/v8/linebot"
	"github.com/line/line-bot-sdk-go/v8/linebot/messaging_api"
)

type Service struct {
	bot           *messaging_api.MessagingApiAPI
	channelSecret string
	adminUserID   string
	accessToken   string
	httpClient    *http.Client
}

func New(channelSecret, accessToken, adminUserID string) (*Service, error) {
	bot, err := messaging_api.NewMessagingApiAPI(accessToken)
	if err != nil {
		return nil, err
	}
	return &Service{
		bot:           bot,
		channelSecret: channelSecret,
		adminUserID:   adminUserID,
		accessToken:   accessToken,
		httpClient:    &http.Client{},
	}, nil
}

// ReplyText sends a simple text reply
func (s *Service) ReplyText(replyToken, text string) error {
	_, err := s.bot.ReplyMessage(&messaging_api.ReplyMessageRequest{
		ReplyToken: replyToken,
		Messages:   []messaging_api.MessageInterface{&messaging_api.TextMessage{Text: text}},
	})
	return err
}

// ReplyImage sends an image via the (free) Reply API. URLs follow the same
// rules as PushImage. Caller must have a fresh replyToken from a recent
// inbound webhook event — token is single-use, expires server-side.
func (s *Service) ReplyImage(replyToken, originalContentURL, previewImageURL string) error {
	if replyToken == "" {
		return fmt.Errorf("replyToken required")
	}
	if originalContentURL == "" {
		return fmt.Errorf("originalContentURL required")
	}
	if previewImageURL == "" {
		previewImageURL = originalContentURL
	}
	_, err := s.bot.ReplyMessage(&messaging_api.ReplyMessageRequest{
		ReplyToken: replyToken,
		Messages: []messaging_api.MessageInterface{
			&messaging_api.ImageMessage{
				OriginalContentUrl: originalContentURL,
				PreviewImageUrl:    previewImageURL,
			},
		},
	})
	return err
}

// IsReplyTokenError reports whether err looks like LINE rejected the
// replyToken (expired, invalid, or already used). Caller falls back to Push.
//
// LINE's 400 errors carry a JSON body like {"message":"Invalid reply token"}
// which the SDK surfaces in err.Error(). We match a few known phrasings —
// LINE may add new variants over time, so be permissive.
//
// We intentionally do NOT match generic 401/429 here:
//   - 401 (auth) means the token is bad → Push will also fail; bubble error up
//   - 429 (rate limit) is transient; falling back to Push burns quota for
//     no reason, so we return false and let the caller surface the error
func IsReplyTokenError(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	// "Invalid reply token" / "The reply token has expired" / "reply token has been used"
	if strings.Contains(s, "reply token") {
		return true
	}
	return false
}

// PushAdmin sends a push message to the admin user
func (s *Service) PushAdmin(text string) error {
	if s.adminUserID == "" {
		return nil
	}
	_, err := s.bot.PushMessage(
		&messaging_api.PushMessageRequest{
			To:       s.adminUserID,
			Messages: []messaging_api.MessageInterface{&messaging_api.TextMessage{Text: text}},
		},
		"", // xLineRetryKey
	)
	return err
}

// PushText sends a plain-text Push to an arbitrary LINE userID. Used by the
// human-chat inbox for admin → customer replies. Push has no reply-window
// constraint so it works any time the user has us as a friend; counts toward
// the LINE OA monthly push quota (200/mo on free Light Plan; Reply API path
// is preferred and is free — see chat_inbox.sendOutgoingText).
func (s *Service) PushText(userID, text string) error {
	if userID == "" {
		return fmt.Errorf("userID required")
	}
	_, err := s.bot.PushMessage(
		&messaging_api.PushMessageRequest{
			To:       userID,
			Messages: []messaging_api.MessageInterface{&messaging_api.TextMessage{Text: text}},
		},
		"",
	)
	return err
}

// PushImage sends an image Push. Both URLs MUST be HTTPS and reachable from
// LINE's servers (they fetch the bytes when delivering to the customer).
// originalContentUrl: ≤10MB JPEG/PNG/WebP, ≤4096×4096
// previewImageUrl:    ≤1MB JPEG, ≤240×240 — can be the same URL when small
//
// LINE Push API does not support a `file` message type, so PDFs / docs from
// the inbox can't be pushed back to the customer; only image/video/audio.
// Workaround for files: use a Flex Message with a download link (deferred).
func (s *Service) PushImage(userID, originalContentURL, previewImageURL string) error {
	if userID == "" {
		return fmt.Errorf("userID required")
	}
	if originalContentURL == "" {
		return fmt.Errorf("originalContentURL required")
	}
	if previewImageURL == "" {
		previewImageURL = originalContentURL
	}
	_, err := s.bot.PushMessage(
		&messaging_api.PushMessageRequest{
			To: userID,
			Messages: []messaging_api.MessageInterface{
				&messaging_api.ImageMessage{
					OriginalContentUrl: originalContentURL,
					PreviewImageUrl:    previewImageURL,
				},
			},
		},
		"",
	)
	return err
}

// UserProfile is the subset of /v2/bot/profile/{userId} we care about.
type UserProfile struct {
	DisplayName   string `json:"displayName"`
	PictureURL    string `json:"pictureUrl"`
	StatusMessage string `json:"statusMessage"`
	UserID        string `json:"userId"`
}

// BotInfo is the subset of /v2/bot/info we care about — used to verify the
// access_token is valid and to cache the bot's own user ID for webhook routing.
type BotInfo struct {
	UserID      string `json:"userId"`
	BasicID     string `json:"basicId"`
	PremiumID   string `json:"premiumId"`
	DisplayName string `json:"displayName"`
	PictureURL  string `json:"pictureUrl"`
	ChatMode    string `json:"chatMode"`
	MarkAsRead  string `json:"markAsReadMode"`
}

// GetBotInfo calls /v2/bot/info — used by /settings/line-oa "Test" button to
// verify a token is alive and to capture bot_user_id.
func (s *Service) GetBotInfo() (*BotInfo, error) {
	req, err := http.NewRequest("GET", "https://api.line.me/v2/bot/info", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+s.accessToken)
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get bot info: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get bot info status %d: %s", resp.StatusCode, body)
	}
	var info BotInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("decode bot info: %w", err)
	}
	return &info, nil
}

// MarkMessagesAsRead calls POST /v2/bot/message/markAsRead — sends a read
// receipt to the customer so their LINE app shows "อ่านแล้ว" under our
// last message. ⚠ Premium-only feature (LINE Official Account Plus): Free
// OA returns 403. Caller should toggle this on per-OA only when they've
// upgraded; we return the error so the caller can log/swallow it.
//
// Body shape per LINE docs:
//   { "chat": { "type": "user", "userId": "Uxxxxxxx" } }
func (s *Service) MarkMessagesAsRead(userID string) error {
	if userID == "" {
		return fmt.Errorf("userID required")
	}
	body := map[string]any{
		"chat": map[string]string{"type": "user", "userId": userID},
	}
	buf, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequest("POST", "https://api.line.me/v2/bot/message/markAsRead", strings.NewReader(string(buf)))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.accessToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("markAsRead: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("markAsRead status %d: %s", resp.StatusCode, bodyBytes)
	}
	return nil
}

// GetProfile fetches the LINE display name + avatar for a userID. Used on
// first contact (when we upsert a chat_conversations row) and via the manual
// "🔄 รีเฟรช profile" button in the inbox UI.
//
// Returns nil profile (no error) when the user has us blocked or LINE returns
// 404 — caller should fall back to displaying the userID.
func (s *Service) GetProfile(userID string) (*UserProfile, error) {
	if userID == "" {
		return nil, fmt.Errorf("userID required")
	}
	req, err := http.NewRequest("GET", "https://api.line.me/v2/bot/profile/"+userID, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+s.accessToken)
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get profile: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get profile status %d: %s", resp.StatusCode, body)
	}
	var p UserProfile
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		return nil, fmt.Errorf("decode profile: %w", err)
	}
	return &p, nil
}

// ValidateSignature verifies X-Line-Signature header
func (s *Service) ValidateSignature(body []byte, signature string) bool {
	return linebot.ValidateSignature(s.channelSecret, signature, body)
}

// DownloadContent downloads message content from LINE Content API
func (s *Service) DownloadContent(messageID string) ([]byte, string, error) {
	req, err := http.NewRequest("GET",
		"https://api-data.line.me/v2/bot/message/"+messageID+"/content", nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Authorization", "Bearer "+s.accessToken)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("download content: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, "", fmt.Errorf("download content status %d: %s", resp.StatusCode, body)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}

	contentType := resp.Header.Get("Content-Type")
	// Strip charset: "image/jpeg; charset=utf-8" → "image/jpeg"
	if idx := strings.Index(contentType, ";"); idx != -1 {
		contentType = strings.TrimSpace(contentType[:idx])
	}
	return data, contentType, nil
}

// BillPreviewItem holds display data for the Flex Message preview
type BillPreviewItem struct {
	RawName  string
	ItemCode string
	Qty      float64
	Unit     string
	Price    float64
}

// ReplyBillPreview sends a Flex Message showing bill items with confirm/cancel buttons
func (s *Service) ReplyBillPreview(replyToken, billID, customerName string, items []BillPreviewItem, hasAnomaly bool) error {
	contents := s.buildPreviewFlex(billID, customerName, items, hasAnomaly)
	contentsJSON, err := json.Marshal(contents)
	if err != nil {
		return err
	}
	return s.replyFlex(replyToken, "ตรวจสอบใบสั่งซื้อ", contentsJSON)
}

// ReplySuccessFlex sends a success Flex Message after bill is sent to SML
func (s *Service) ReplySuccessFlex(replyToken, docNo string, itemCount int) error {
	docLine := "รอรับเลขที่เอกสารจาก SML"
	if docNo != "" {
		docLine = "เลขที่: " + docNo
	}
	bubble := map[string]interface{}{
		"type": "bubble",
		"header": map[string]interface{}{
			"type": "box", "layout": "vertical",
			"backgroundColor": "#1DB954",
			"contents": []interface{}{
				map[string]interface{}{"type": "text", "text": "✅ บันทึกสำเร็จ", "color": "#ffffff", "weight": "bold", "size": "lg"},
			},
		},
		"body": map[string]interface{}{
			"type": "box", "layout": "vertical", "spacing": "sm",
			"contents": []interface{}{
				map[string]interface{}{"type": "text", "text": docLine, "size": "sm", "weight": "bold"},
				map[string]interface{}{"type": "text", "text": fmt.Sprintf("รายการสินค้า: %d รายการ", itemCount), "size": "sm", "color": "#666666"},
				map[string]interface{}{"type": "text", "text": "ส่งเข้า SML เรียบร้อยแล้วครับ 🎉", "size": "sm", "wrap": true},
			},
		},
	}
	contentsJSON, _ := json.Marshal(bubble)
	return s.replyFlex(replyToken, "บันทึกสำเร็จ", contentsJSON)
}

// PushSuccessToUser pushes a success message to a specific user (when replyToken is gone)
func (s *Service) PushSuccessToUser(userID, docNo string, itemCount int) {
	if userID == "" {
		return
	}
	docLine := "รอรับเลขที่เอกสารจาก SML"
	if docNo != "" {
		docLine = "เลขที่: " + docNo
	}
	text := fmt.Sprintf("✅ บันทึกสำเร็จ!\n%s\nสินค้า: %d รายการ\nส่งเข้า SML เรียบร้อยแล้วครับ", docLine, itemCount)
	s.bot.PushMessage(
		&messaging_api.PushMessageRequest{
			To:       userID,
			Messages: []messaging_api.MessageInterface{&messaging_api.TextMessage{Text: text}},
		},
		"",
	)
}

// ReplySuccess sends a success text reply after bill is sent to SML (kept for backward compat)
func (s *Service) ReplySuccess(replyToken, docNo string, itemCount int) error {
	return s.ReplySuccessFlex(replyToken, docNo, itemCount)
}

// buildPreviewFlex constructs the Flex Message bubble as a Go map (marshaled safely)
func (s *Service) buildPreviewFlex(billID, customerName string, items []BillPreviewItem, hasAnomaly bool) map[string]interface{} {
	bodyContents := []interface{}{
		map[string]interface{}{
			"type": "box", "layout": "horizontal",
			"contents": []interface{}{
				map[string]interface{}{"type": "text", "text": "ลูกค้า", "color": "#888888", "size": "xs", "flex": 2},
				map[string]interface{}{"type": "text", "text": customerName, "size": "xs", "weight": "bold", "flex": 5, "wrap": true},
			},
		},
		map[string]interface{}{"type": "separator", "margin": "sm"},
		map[string]interface{}{
			"type": "box", "layout": "horizontal", "margin": "sm",
			"contents": []interface{}{
				map[string]interface{}{"type": "text", "text": "รายการ", "color": "#888888", "size": "xs", "flex": 3},
				map[string]interface{}{"type": "text", "text": "จำนวน", "color": "#888888", "size": "xs", "flex": 2, "align": "center"},
				map[string]interface{}{"type": "text", "text": "รวม", "color": "#888888", "size": "xs", "flex": 2, "align": "end"},
			},
		},
	}

	var total float64
	for _, item := range items {
		lineTotal := item.Price * item.Qty
		total += lineTotal
		nameText := item.RawName
		if item.ItemCode != "" && item.ItemCode != item.RawName {
			nameText = item.RawName + "\n(" + item.ItemCode + ")"
		}
		bodyContents = append(bodyContents, map[string]interface{}{
			"type": "box", "layout": "horizontal", "margin": "xs",
			"contents": []interface{}{
				map[string]interface{}{"type": "text", "text": nameText, "flex": 3, "size": "xs", "wrap": true},
				map[string]interface{}{"type": "text", "text": fmt.Sprintf("%.0f %s", item.Qty, item.Unit), "flex": 2, "size": "xs", "align": "center"},
				map[string]interface{}{"type": "text", "text": fmt.Sprintf("%.0f฿", lineTotal), "flex": 2, "size": "xs", "align": "end"},
			},
		})
	}

	bodyContents = append(bodyContents, map[string]interface{}{"type": "separator", "margin": "sm"})

	if hasAnomaly {
		bodyContents = append(bodyContents, map[string]interface{}{
			"type": "box", "layout": "horizontal", "margin": "sm",
			"contents": []interface{}{
				map[string]interface{}{"type": "text", "text": "⚠️ พบความผิดปกติ — โปรดตรวจสอบ", "size": "xs", "color": "#FF6B6B", "wrap": true},
			},
		})
	}

	bodyContents = append(bodyContents, map[string]interface{}{
		"type": "box", "layout": "horizontal", "margin": "sm",
		"contents": []interface{}{
			map[string]interface{}{"type": "text", "text": "ยอดรวม", "weight": "bold", "size": "sm", "flex": 3},
			map[string]interface{}{"type": "text", "text": fmt.Sprintf("%.0f บาท", total), "weight": "bold", "size": "sm", "color": "#1DB954", "align": "end", "flex": 4},
		},
	})

	return map[string]interface{}{
		"type": "bubble",
		"header": map[string]interface{}{
			"type": "box", "layout": "vertical",
			"backgroundColor": "#1a73e8",
			"contents": []interface{}{
				map[string]interface{}{"type": "text", "text": "📋 ยืนยันใบสั่งซื้อ", "weight": "bold", "size": "md", "color": "#ffffff"},
			},
		},
		"body": map[string]interface{}{
			"type": "box", "layout": "vertical", "spacing": "sm",
			"contents": bodyContents,
		},
		"footer": map[string]interface{}{
			"type": "box", "layout": "horizontal", "spacing": "sm",
			"contents": []interface{}{
				map[string]interface{}{
					"type": "button", "style": "primary", "color": "#1DB954", "height": "sm",
					"action": map[string]interface{}{
						"type": "postback", "label": "✅ ยืนยัน",
						"data": "action=confirm&bill_id=" + billID,
					},
				},
				map[string]interface{}{
					"type": "button", "style": "secondary", "height": "sm",
					"action": map[string]interface{}{
						"type": "postback", "label": "❌ ยกเลิก",
						"data": "action=cancel&bill_id=" + billID,
					},
				},
			},
		},
	}
}

// replyFlex sends a Flex Message via direct HTTP to the LINE Reply API
func (s *Service) replyFlex(replyToken, altText string, contentsJSON []byte) error {
	payload := map[string]interface{}{
		"replyToken": replyToken,
		"messages": []interface{}{
			map[string]interface{}{
				"type":     "flex",
				"altText":  altText,
				"contents": json.RawMessage(contentsJSON),
			},
		},
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", "https://api.line.me/v2/bot/message/reply",
		strings.NewReader(string(payloadJSON)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.accessToken)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("reply flex status %d: %s", resp.StatusCode, b)
	}
	return nil
}
