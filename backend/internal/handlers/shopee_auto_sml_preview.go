package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"nexflow/internal/models"
)

const autoSMLPreviewTTL = 10 * time.Minute

type autoSMLPreviewClaims struct {
	ShopID         int64  `json:"shop_id"`
	TriggerStatus  string `json:"trigger_status"`
	ConfigVersion  int64  `json:"config_version"`
	RouteSignature string `json:"route_signature"`
	ExpiresAt      int64  `json:"expires_at"`
}

func (h *ShopeeRealtimeHandler) AutoSMLSettingPreview(c *gin.Context) {
	if !h.enabled(c) {
		return
	}
	shopID, err := strconv.ParseInt(strings.TrimSpace(c.Param("shop_id")), 10, 64)
	if err != nil || shopID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "shop_id ไม่ถูกต้อง"})
		return
	}
	var req struct {
		TriggerStatus string `json:"trigger_status"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ข้อมูล preview ไม่ถูกต้อง"})
		return
	}
	setting, err := h.autoSMLRepo.GetSetting(c.Request.Context(), shopID)
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{"error": "ไม่พบร้าน Shopee"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "โหลดการตั้งค่า Auto SML ไม่สำเร็จ"})
		return
	}
	trigger := models.NormalizeShopeeAutoSMLTriggerStatus(req.TriggerStatus)
	if trigger == "" {
		trigger = models.NormalizeShopeeAutoSMLTriggerStatus(setting.TriggerStatus)
	}
	if trigger == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "สถานะเริ่มสร้างบิลไม่ถูกต้อง"})
		return
	}
	if code, message := h.autoSMLPreflight(c.Request.Context(), shopID); code != "" {
		c.JSON(http.StatusConflict, gin.H{"error": message, "code": code})
		return
	}
	routeSignature := h.realtimeRouteSignature(c.Request.Context())
	expiresAt := time.Now().Add(autoSMLPreviewTTL)
	claims := autoSMLPreviewClaims{
		ShopID: shopID, TriggerStatus: trigger, ConfigVersion: setting.ConfigVersion,
		RouteSignature: routeSignature, ExpiresAt: expiresAt.Unix(),
	}
	token, err := signAutoSMLPreviewToken(claims, h.cfg.JWTSecret)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "สร้างหลักฐาน preview ไม่สำเร็จ"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"preview_token": token, "expires_at": expiresAt, "shop_id": shopID,
		"trigger_status": trigger, "config_version": setting.ConfigVersion,
		"route_signature": routeSignature,
		"impact": gin.H{
			"new_orders_only": true, "historical_backfill": false,
			"auto_sml_will_resume": true, "document_profile_mode": h.cfg.SMLDocumentProfileMode,
		},
		"warnings": []string{
			"มีผลเฉพาะออเดอร์ที่เข้าสถานะหลังยืนยัน",
			"เมื่อสร้างเอกสาร SML แล้ว ระบบจะไม่ลบเอกสารนั้นอัตโนมัติ",
		},
	})
}

func signAutoSMLPreviewToken(claims autoSMLPreviewClaims, secret string) (string, error) {
	if len(secret) < 32 {
		return "", errors.New("preview signing key is not configured")
	}
	body, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(body)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(encoded))
	return encoded + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func validateAutoSMLPreviewToken(token, secret string, expected autoSMLPreviewClaims, now time.Time) error {
	if len(secret) < 32 {
		return errors.New("preview signing key is not configured")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return errors.New("preview token is invalid")
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return errors.New("preview token is invalid")
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(parts[0]))
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return errors.New("preview token signature is invalid")
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return errors.New("preview token is invalid")
	}
	var claims autoSMLPreviewClaims
	if err := json.Unmarshal(body, &claims); err != nil {
		return errors.New("preview token is invalid")
	}
	if claims.ExpiresAt <= now.Unix() {
		return errors.New("preview token expired")
	}
	if claims.ShopID != expected.ShopID || claims.TriggerStatus != expected.TriggerStatus ||
		claims.ConfigVersion != expected.ConfigVersion || claims.RouteSignature != expected.RouteSignature {
		return errors.New("settings changed after preview")
	}
	return nil
}
