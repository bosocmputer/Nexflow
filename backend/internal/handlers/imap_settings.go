package handlers

import (
	"context"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"nexflow/internal/models"
	"nexflow/internal/repository"
	emailservice "nexflow/internal/services/email"
)

// IMAPSettingsHandler exposes CRUD + test/poll for imap_accounts. Admin only.
type IMAPSettingsHandler struct {
	repo        *repository.ImapAccountRepo
	jobRepo     *repository.IMAPPollJobRepo
	coordinator *emailservice.Coordinator
	logger      *zap.Logger
}

type resetIMAPProgressRequest struct {
	LookbackDays *int `json:"lookback_days"`
	PollNow      bool `json:"poll_now"`
}

func NewIMAPSettingsHandler(
	repo *repository.ImapAccountRepo,
	jobRepo *repository.IMAPPollJobRepo,
	coordinator *emailservice.Coordinator,
	logger *zap.Logger,
) *IMAPSettingsHandler {
	return &IMAPSettingsHandler{repo: repo, jobRepo: jobRepo, coordinator: coordinator, logger: logger}
}

// List returns all accounts. Passwords are scrubbed before sending to the client
// so admins editing a row never see the existing password (re-enter to change).
func (h *IMAPSettingsHandler) List(c *gin.Context) {
	accounts, err := h.repo.ListAll()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if accounts == nil {
		accounts = []*models.IMAPAccount{}
	}
	for _, a := range accounts {
		a.Password = ""
		if h.coordinator != nil {
			a.PollRunning = h.coordinator.IsPolling(a.ID)
		}
	}
	c.JSON(http.StatusOK, gin.H{"data": accounts})
}

// Get returns a single account, password scrubbed.
func (h *IMAPSettingsHandler) Get(c *gin.Context) {
	id := c.Param("id")
	a, err := h.repo.GetByID(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if a == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "account not found"})
		return
	}
	a.Password = ""
	if h.coordinator != nil {
		a.PollRunning = h.coordinator.IsPolling(a.ID)
	}
	c.JSON(http.StatusOK, a)
}

// Create inserts a new account and starts its poller.
func (h *IMAPSettingsHandler) Create(c *gin.Context) {
	var in models.IMAPAccountUpsert
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	normalizeIMAPUpsert(&in)
	if in.Password == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "password required for new account"})
		return
	}
	if msg := validateIMAPUpsert(in, true); msg != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": msg})
		return
	}
	a := upsertToModel(in)
	if err := h.repo.Create(a); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := h.coordinator.ReloadAccount(a.ID); err != nil {
		h.logger.Warn("imap_create_reload_failed", zap.String("id", a.ID), zap.Error(err))
	}
	a.Password = ""
	c.JSON(http.StatusCreated, a)
}

// Update overwrites a row. Empty password means "keep existing".
func (h *IMAPSettingsHandler) Update(c *gin.Context) {
	id := c.Param("id")
	var in models.IMAPAccountUpsert
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	normalizeIMAPUpsert(&in)
	if msg := validateIMAPUpsert(in, false); msg != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": msg})
		return
	}
	a := upsertToModel(in)
	if err := h.repo.Update(id, a); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := h.coordinator.ReloadAccount(id); err != nil {
		h.logger.Warn("imap_update_reload_failed", zap.String("id", id), zap.Error(err))
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// Delete removes the row and stops its poller.
func (h *IMAPSettingsHandler) Delete(c *gin.Context) {
	id := c.Param("id")
	if err := h.repo.Delete(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	h.coordinator.RemoveAccount(id)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// PollNow triggers an immediate poll for the account. Useful after edits or
// when admin wants to verify the connection without waiting for the next tick.
func (h *IMAPSettingsHandler) PollNow(c *gin.Context) {
	id := c.Param("id")
	res, err := h.coordinator.PollNow(id)
	if err != nil {
		if strings.Contains(err.Error(), "poll already running") {
			c.JSON(http.StatusConflict, gin.H{"error": "กล่องเมลนี้กำลังดึงอีเมลอยู่แล้ว กรุณารอสักครู่แล้วรีเฟรชสถานะ"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	resp := imapPollResponse(res)
	c.JSON(http.StatusOK, resp)
}

func (h *IMAPSettingsHandler) CreatePollJob(c *gin.Context) {
	id := c.Param("id")
	job, err := h.coordinator.StartPollJob(id, c.GetString("user_id"), c.GetString("user_email"))
	if err != nil {
		if strings.Contains(err.Error(), "account not found") {
			c.JSON(http.StatusNotFound, gin.H{"error": "account not found"})
			return
		}
		if strings.Contains(err.Error(), "disabled") {
			c.JSON(http.StatusBadRequest, gin.H{"error": "กล่องเมลนี้ปิดใช้งานอยู่"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"job_id": job.ID, "job": job})
}

func (h *IMAPSettingsHandler) ListActivePollJobs(c *gin.Context) {
	jobs, err := h.jobRepo.ListActive()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": jobs})
}

func (h *IMAPSettingsHandler) GetPollJob(c *gin.Context) {
	job, err := h.jobRepo.Get(c.Param("job_id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if job == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "imap poll job not found"})
		return
	}
	c.JSON(http.StatusOK, job)
}

// ResetProgress clears the resumable IMAP cursor so admins can intentionally
// re-read a lookback window. It does not delete processed-message history or
// any bills, so duplicate protection still applies.
func (h *IMAPSettingsHandler) ResetProgress(c *gin.Context) {
	id := c.Param("id")
	var in resetIMAPProgressRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if in.LookbackDays != nil && (*in.LookbackDays < 1 || *in.LookbackDays > 90) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "lookback_days must be between 1 and 90"})
		return
	}
	if err := h.repo.ResetPollProgress(id, in.LookbackDays); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if in.PollNow {
		job, err := h.coordinator.StartPollJob(id, c.GetString("user_id"), c.GetString("user_email"))
		if err != nil {
			if strings.Contains(err.Error(), "account not found") {
				c.JSON(http.StatusNotFound, gin.H{"error": "account not found"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusAccepted, gin.H{"job_id": job.ID, "job": job})
		return
	}
	if err := h.coordinator.ReloadAccount(id); err != nil {
		h.logger.Warn("imap_reset_progress_reload_failed", zap.String("id", id), zap.Error(err))
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func imapPollResponse(res emailservice.PollResult) gin.H {
	resp := gin.H{
		"trace_id":       res.TraceID,
		"messages_found": res.MessagesFound,
		"processed":      res.Processed,
		"skipped":        res.Skipped,
		"duration_ms":    res.Duration.Milliseconds(),
		"status":         res.Status(),
		"summary":        res.Summary,
		"details":        res.Details,
		"last_seen_uid":  res.LastSeenUID,
		"limited":        res.Limited,
		"backlog":        res.Backlog,
	}
	if res.Err != nil {
		resp["error"] = res.Err.Error()
	} else if len(res.ProcessWarnings) > 0 {
		resp["error"] = res.ProcessWarnings[0]
		resp["warnings"] = res.ProcessWarnings
	}
	return resp
}

// TestConnection runs a dry connect+auth+select cycle WITHOUT saving anything.
// Body is the same shape as Create. Used by the "ทดสอบการเชื่อมต่อ" button.
func (h *IMAPSettingsHandler) TestConnection(c *gin.Context) {
	var in models.IMAPAccountUpsert
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	normalizeIMAPUpsert(&in)
	a := upsertToModel(in)
	if a.Password == "" {
		// Editing an existing row without re-typing password — pull from DB.
		if id := c.Query("id"); id != "" {
			if existing, _ := h.repo.GetByID(id); existing != nil {
				a.Password = existing.Password
			}
		}
	}
	if a.Password == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "password required"})
		return
	}
	if msg := validateIMAPAccount(a, true); msg != "" {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": msg})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 12*time.Second)
	defer cancel()

	start := time.Now()
	if err := h.coordinator.TestConnection(ctx, a); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"ok":          false,
			"error":       friendlyIMAPError(err.Error()),
			"duration_ms": time.Since(start).Milliseconds(),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"ok":          true,
		"duration_ms": time.Since(start).Milliseconds(),
	})
}

// ListFolders returns the IMAP mailbox names for the supplied account creds.
// Body shape = same as Create. Used to populate the folder dropdown.
func (h *IMAPSettingsHandler) ListFolders(c *gin.Context) {
	var in models.IMAPAccountUpsert
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	normalizeIMAPUpsert(&in)
	a := upsertToModel(in)
	if a.Password == "" {
		if id := c.Query("id"); id != "" {
			if existing, _ := h.repo.GetByID(id); existing != nil {
				a.Password = existing.Password
			}
		}
	}
	if a.Password == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "password required"})
		return
	}
	if msg := validateIMAPAccount(a, true); msg != "" {
		c.JSON(http.StatusOK, gin.H{"folders": []string{}, "error": msg})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 12*time.Second)
	defer cancel()

	folders, err := emailservice.ListMailboxes(ctx, a)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"folders": folders, "error": friendlyIMAPError(err.Error())})
		return
	}
	c.JSON(http.StatusOK, gin.H{"folders": folders})
}

func normalizeIMAPUpsert(in *models.IMAPAccountUpsert) {
	in.Name = strings.TrimSpace(in.Name)
	in.Host = strings.ToLower(strings.TrimSpace(in.Host))
	in.Username = strings.TrimSpace(in.Username)
	in.Mailbox = strings.TrimSpace(in.Mailbox)
	in.FilterFrom = strings.TrimSpace(in.FilterFrom)
	in.FilterSubjects = strings.TrimSpace(in.FilterSubjects)
	in.ShopeeDomains = strings.TrimSpace(in.ShopeeDomains)
	if isGmailHost(in.Host) {
		in.Password = normalizeGmailAppPassword(in.Password)
	}
}

func normalizeGmailAppPassword(password string) string {
	var b strings.Builder
	for _, r := range password {
		if unicode.IsSpace(r) || r == '-' {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func isGmailHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	return host == "imap.gmail.com" || strings.Contains(host, ".gmail.") || strings.Contains(host, "google")
}

func validateIMAPUpsert(in models.IMAPAccountUpsert, requirePassword bool) string {
	if requirePassword && strings.TrimSpace(in.Password) == "" {
		return "กรุณากรอก App Password"
	}
	if isGmailHost(in.Host) && in.Password != "" && len([]rune(in.Password)) != 16 {
		return "Gmail App Password ควรมี 16 ตัวอักษรหลังลบช่องว่าง เช่น qzqqvwqbzydodtsi ไม่ใช่รหัสผ่าน Gmail ปกติ"
	}
	return ""
}

func validateIMAPAccount(a *models.IMAPAccount, requirePassword bool) string {
	return validateIMAPUpsert(models.IMAPAccountUpsert{
		Host:     a.Host,
		Password: a.Password,
	}, requirePassword)
}

func friendlyIMAPError(msg string) string {
	lower := strings.ToLower(msg)
	if strings.Contains(lower, "authenticationfailed") ||
		strings.Contains(lower, "authenticate") ||
		strings.Contains(lower, "invalid credentials") ||
		strings.Contains(lower, "password") {
		return "Gmail/IMAP ยืนยันตัวตนไม่ผ่าน: ให้ใช้ App Password 16 ตัวจาก Google ไม่ใช่รหัสผ่าน Gmail ปกติ, ตรวจว่าเปิด 2-Step Verification แล้ว, เปิด IMAP ใน Gmail แล้ว, และถ้า copy รหัสแบบมีช่องว่าง ระบบจะลบช่องว่างให้อัตโนมัติ"
	}
	return msg
}

func upsertToModel(in models.IMAPAccountUpsert) *models.IMAPAccount {
	mailbox := in.Mailbox
	if mailbox == "" {
		mailbox = "INBOX"
	}
	return &models.IMAPAccount{
		Name:                in.Name,
		Host:                in.Host,
		Port:                in.Port,
		Username:            in.Username,
		Password:            in.Password,
		Mailbox:             mailbox,
		FilterFrom:          in.FilterFrom,
		FilterSubjects:      in.FilterSubjects,
		Channel:             in.Channel,
		ShopeeDomains:       in.ShopeeDomains,
		LookbackDays:        in.LookbackDays,
		PollIntervalSeconds: in.PollIntervalSeconds,
		Enabled:             in.Enabled,
	}
}
