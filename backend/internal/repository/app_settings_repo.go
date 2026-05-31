package repository

import (
	"database/sql"
	"strconv"
	"strings"

	"nexflow/internal/config"
)

type AppSetting struct {
	Key       string `json:"key"`
	Value     string `json:"value"`
	IsSecret  bool   `json:"is_secret"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

type AppSettingsRepo struct {
	db *sql.DB
}

func NewAppSettingsRepo(db *sql.DB) *AppSettingsRepo {
	return &AppSettingsRepo{db: db}
}

func (r *AppSettingsRepo) All() (map[string]AppSetting, error) {
	rows, err := r.db.Query(`
		SELECT key, value, is_secret, updated_at::text
		  FROM app_settings
		 ORDER BY key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]AppSetting{}
	for rows.Next() {
		var s AppSetting
		if err := rows.Scan(&s.Key, &s.Value, &s.IsSecret, &s.UpdatedAt); err != nil {
			return nil, err
		}
		out[s.Key] = s
	}
	return out, rows.Err()
}

func (r *AppSettingsRepo) UpsertMany(values map[string]string, secretKeys map[string]bool, updatedBy string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for key, value := range values {
		isSecret := secretKeys[key]
		var userID any
		if updatedBy != "" {
			userID = updatedBy
		}
		if _, err := tx.Exec(`
			INSERT INTO app_settings (key, value, is_secret, updated_by, updated_at)
			VALUES ($1, $2, $3, $4, NOW())
			ON CONFLICT (key) DO UPDATE
			   SET value = EXCLUDED.value,
			       is_secret = EXCLUDED.is_secret,
			       updated_by = EXCLUDED.updated_by,
			       updated_at = NOW()`,
			key, value, isSecret, userID,
		); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (r *AppSettingsRepo) ApplyToConfig(cfg *config.Config) error {
	settings, err := r.All()
	if err != nil {
		return err
	}
	get := func(key string) string {
		return strings.TrimSpace(settings[key].Value)
	}
	if v := get("instance.name"); v != "" {
		// Currently displayed in UI only. Kept in DB; no config field needed.
	}
	if v := get("ai.openrouter_api_key"); v != "" {
		cfg.OpenRouterAPIKey = v
	}
	if v := get("ai.openrouter_model"); v != "" {
		cfg.OpenRouterModel = v
	}
	if v := get("ai.openrouter_fallback_model"); v != "" {
		cfg.OpenRouterFallback = v
	}
	if v := get("ai.openrouter_audio_model"); v != "" {
		cfg.OpenRouterAudioModel = v
	}
	if v := get("sml.rest_base_url"); v != "" {
		cfg.ShopeeSMLURL = v
	}
	if v := get("sml.database"); v != "" {
		cfg.ShopeeSMLDatabase = v
	}
	if v := get("line.notify_channel_secret"); v != "" {
		cfg.LineChannelSecret = v
	}
	if v := get("line.notify_channel_access_token"); v != "" {
		cfg.LineChannelAccessToken = v
	}
	if v := get("line.notify_admin_user_id"); v != "" {
		cfg.LineAdminUserID = v
	}
	if v := get("automation.auto_confirm_threshold"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.AutoConfirmThreshold = f
		}
	}
	return nil
}

func RuntimeSettingValues(cfg *config.Config) map[string]string {
	return map[string]string{
		"sml.rest_base_url":                 cfg.ShopeeSMLURL,
		"sml.database":                      cfg.ShopeeSMLDatabase,
		"line.notify_channel_secret":        cfg.LineChannelSecret,
		"line.notify_channel_access_token":  cfg.LineChannelAccessToken,
		"line.notify_admin_user_id":         cfg.LineAdminUserID,
		"ai.openrouter_api_key":             cfg.OpenRouterAPIKey,
		"ai.openrouter_model":               cfg.OpenRouterModel,
		"ai.openrouter_fallback_model":      cfg.OpenRouterFallback,
		"ai.openrouter_audio_model":         cfg.OpenRouterAudioModel,
		"automation.auto_confirm_threshold": strconv.FormatFloat(cfg.AutoConfirmThreshold, 'f', -1, 64),
	}
}



// GetValue returns the stored value for key, or "" if the key is not found.
func (r *AppSettingsRepo) GetValue(key string) (string, error) {
	var value string
	err := r.db.QueryRow(
		`SELECT COALESCE(value,'') FROM app_settings WHERE key=$1`, key,
	).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return strings.TrimSpace(value), err
}

func (r *AppSettingsRepo) PendingRestart(cfg *config.Config) (bool, []string, error) {
	settings, err := r.All()
	if err != nil {
		return false, nil, err
	}
	runtime := RuntimeSettingValues(cfg)
	keys := []string{}
	for key, activeValue := range runtime {
		savedValue := strings.TrimSpace(settings[key].Value)
		if savedValue == "" {
			continue
		}
		if savedValue != strings.TrimSpace(activeValue) {
			keys = append(keys, key)
		}
	}
	return len(keys) > 0, keys, nil
}
