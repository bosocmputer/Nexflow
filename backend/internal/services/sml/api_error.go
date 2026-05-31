package sml

import "encoding/json"

func apiErrorMessage(v any) string {
	if v == nil {
		return ""
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	var body struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(b, &body); err != nil {
		return ""
	}
	if body.Message != "" {
		return body.Message
	}
	return body.Code
}
