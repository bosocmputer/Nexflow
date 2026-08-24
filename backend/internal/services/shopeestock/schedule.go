package shopeestock

import (
	"fmt"
	"strings"
	"time"
)

const (
	minimumStockIntervalSeconds = 5 * 60
	maximumStockIntervalSeconds = 12 * 7 * 24 * 60 * 60
)

var bangkokLocation = func() *time.Location {
	location, err := time.LoadLocation("Asia/Bangkok")
	if err != nil {
		return time.FixedZone("Asia/Bangkok", 7*60*60)
	}
	return location
}()

func normalizeSchedule(request SettingsUpdate) (SettingsUpdate, error) {
	request.ScheduleMode = strings.ToLower(strings.TrimSpace(request.ScheduleMode))
	if request.ScheduleMode == "" {
		request.ScheduleMode = "interval"
	}
	switch request.ScheduleMode {
	case "interval":
		if request.IntervalSeconds < minimumStockIntervalSeconds || request.IntervalSeconds > maximumStockIntervalSeconds {
			return request, invalid("รอบซิงก์ต้องอยู่ระหว่าง 5 นาทีถึง 12 สัปดาห์")
		}
		request.MonthlyInterval = 1
		request.MonthlyDay = 1
		request.MonthlyTime = "00:00"
		if request.IntervalSeconds >= 24*60*60 && !request.ScheduleRiskAcknowledged {
			return request, invalid("กรุณายืนยันความเสี่ยงเมื่อกำหนดรอบซิงก์ตั้งแต่ 1 วันขึ้นไป")
		}
		if request.IntervalSeconds < 24*60*60 {
			request.ScheduleRiskAcknowledged = false
		}
	case "monthly":
		if request.MonthlyInterval < 1 || request.MonthlyInterval > 12 {
			return request, invalid("รอบรายเดือนต้องอยู่ระหว่าง 1-12 เดือน")
		}
		if request.MonthlyDay < 1 || request.MonthlyDay > 28 {
			return request, invalid("วันที่ซิงก์รายเดือนต้องอยู่ระหว่างวันที่ 1-28")
		}
		if _, _, err := parseScheduleTime(request.MonthlyTime); err != nil {
			return request, invalid("เวลาซิงก์รายเดือนไม่ถูกต้อง")
		}
		if !request.ScheduleRiskAcknowledged {
			return request, invalid("กรุณายืนยันความเสี่ยงของรอบซิงก์รายเดือน")
		}
		// Keep the legacy field populated for older clients. Monthly scheduling uses
		// next_run_at and never treats a month as a fixed number of seconds.
		request.IntervalSeconds = request.MonthlyInterval * 30 * 24 * 60 * 60
	default:
		return request, invalid("รูปแบบรอบซิงก์ไม่ถูกต้อง")
	}
	return request, nil
}

func preserveScheduleForLegacyRequest(request SettingsUpdate, current *Settings) SettingsUpdate {
	if strings.TrimSpace(request.ScheduleMode) != "" || current == nil {
		return request
	}
	if current.ScheduleMode == "monthly" {
		request.ScheduleMode = current.ScheduleMode
		request.IntervalSeconds = current.IntervalSeconds
		request.MonthlyInterval = current.MonthlyInterval
		request.MonthlyDay = current.MonthlyDay
		request.MonthlyTime = current.MonthlyTime
		request.ScheduleRiskAcknowledged = current.ScheduleRiskAcknowledged
		return request
	}
	// The old UI only sent interval_seconds. Preserve an unchanged long-running
	// schedule so saving an unrelated setting does not suddenly fail validation.
	if request.IntervalSeconds == current.IntervalSeconds && current.IntervalSeconds >= 24*60*60 {
		request.ScheduleRiskAcknowledged = true
	}
	return request
}

func nextScheduledRun(settings Settings, from time.Time) time.Time {
	if settings.ScheduleMode != "monthly" {
		seconds := settings.IntervalSeconds
		if seconds < minimumStockIntervalSeconds {
			seconds = minimumStockIntervalSeconds
		}
		return from.Add(time.Duration(seconds) * time.Second).UTC()
	}

	interval := settings.MonthlyInterval
	if interval < 1 {
		interval = 1
	}
	day := settings.MonthlyDay
	if day < 1 || day > 28 {
		day = 1
	}
	hour, minute, err := parseScheduleTime(settings.MonthlyTime)
	if err != nil {
		hour, minute = 0, 0
	}
	localFrom := from.In(bangkokLocation)
	candidate := time.Date(localFrom.Year(), localFrom.Month(), day, hour, minute, 0, 0, bangkokLocation)
	if !candidate.After(localFrom) {
		candidate = candidate.AddDate(0, interval, 0)
	}
	return candidate.UTC()
}

func nextScheduledRunAfter(settings Settings, from time.Time) time.Time {
	if settings.ScheduleMode != "monthly" || settings.NextRunAt == nil {
		return nextScheduledRun(settings, from)
	}
	interval := settings.MonthlyInterval
	if interval < 1 {
		interval = 1
	}
	candidate := settings.NextRunAt.In(bangkokLocation)
	localFrom := from.In(bangkokLocation)
	for !candidate.After(localFrom) {
		candidate = candidate.AddDate(0, interval, 0)
	}
	return candidate.UTC()
}

func parseScheduleTime(value string) (int, int, error) {
	parsed, err := time.Parse("15:04", strings.TrimSpace(value))
	if err != nil {
		return 0, 0, fmt.Errorf("parse schedule time: %w", err)
	}
	return parsed.Hour(), parsed.Minute(), nil
}

func scheduleChanged(current *Settings, request SettingsUpdate) bool {
	if current == nil {
		return true
	}
	return current.ScheduleMode != request.ScheduleMode ||
		current.IntervalSeconds != request.IntervalSeconds ||
		current.MonthlyInterval != request.MonthlyInterval ||
		current.MonthlyDay != request.MonthlyDay ||
		current.MonthlyTime != request.MonthlyTime
}

func settingsFromUpdate(request SettingsUpdate) Settings {
	return Settings{
		ScheduleMode:    request.ScheduleMode,
		IntervalSeconds: request.IntervalSeconds,
		MonthlyInterval: request.MonthlyInterval,
		MonthlyDay:      request.MonthlyDay,
		MonthlyTime:     request.MonthlyTime,
	}
}
