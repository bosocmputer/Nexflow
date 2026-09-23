package linenotify

import (
	"encoding/json"
	"strings"
	"testing"

	"nexflow/internal/models"
)

func TestBuildTikTokSettlementLineFlexUsesTikTokHierarchyWithoutPII(t *testing.T) {
	in := models.TikTokSettlementLineNotification{
		RunID: "run-1", ShopID: "7494619203789490654", ShopName: "henna_milkford",
		StatementID: "7686691708477769480", PaymentID: "3705459816387741150",
		Currency: "THB", TotalAmount: 2922.57, OrderCount: 4, RCDocNo: "RC26090002", Outcome: "sent",
	}
	alt, flex := BuildTikTokSettlementLineFlex(in, "https://nexflow-aoy.nextstep-soft.com")
	if flex == nil || !strings.Contains(alt, "ส่ง RC TikTok Shop แล้ว") {
		t.Fatalf("expected TikTok settlement Flex payload, alt=%q flex=%v", alt, flex)
	}
	raw, err := json.Marshal(flex)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, want := range []string{"TikTok Shop", "#111817", "henna_milkford", "7686691708477769480", "3705459816387741150", "RC26090002"} {
		if !strings.Contains(text, want) {
			t.Fatalf("TikTok settlement Flex missing %q: %s", want, text)
		}
	}
	for _, forbidden := range []string{"buyer", "phone", "address", "account_number"} {
		if strings.Contains(strings.ToLower(text), forbidden) {
			t.Fatalf("TikTok settlement Flex contains prohibited PII field %q: %s", forbidden, text)
		}
	}
}

func TestBuildTikTokSettlementLineFlexKeepsTerminalFailureActionable(t *testing.T) {
	in := models.TikTokSettlementLineNotification{RunID: "run-2", ShopName: "henna_milkford", Outcome: "failed", ErrorMessage: "SML ปฏิเสธเอกสาร"}
	_, flex := BuildTikTokSettlementLineFlex(in, "")
	raw, _ := json.Marshal(flex)
	if !strings.Contains(string(raw), "สิ่งที่ต้องดำเนินการ") || !strings.Contains(string(raw), "#DC2626") {
		t.Fatalf("failure Flex must describe the next action: %s", raw)
	}
}
