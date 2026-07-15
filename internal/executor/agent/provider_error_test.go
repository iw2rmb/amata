package agent

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
)

func TestNormalizeProviderErrorLine(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		line        string
		wantMatched bool
		wantOK      bool
		wantDetails map[string]any
	}{
		{
			name:        "wrapped message envelope",
			line:        `{"type":"error","message":"{\"error\":{\"message\":\"invalid encrypted\",\"type\":\"invalid_request_error\",\"param\":null,\"code\":\"invalid_encrypted_content\"}}"}`,
			wantMatched: true,
			wantOK:      true,
			wantDetails: map[string]any{
				"message": "invalid encrypted",
				"type":    "invalid_request_error",
				"param":   nil,
				"code":    "invalid_encrypted_content",
			},
		},
		{
			name:        "direct nested error object",
			line:        `{"type":"error","error":{"message":"bad request","type":"invalid_request_error","param":"prompt","code":"bad_prompt"}}`,
			wantMatched: true,
			wantOK:      true,
			wantDetails: map[string]any{
				"message": "bad request",
				"type":    "invalid_request_error",
				"param":   "prompt",
				"code":    "bad_prompt",
			},
		},
		{
			name:        "error event without parseable envelope",
			line:        `{"type":"error","message":"not-json"}`,
			wantMatched: true,
			wantOK:      false,
			wantDetails: map[string]any{
				"message": "not-json",
				"type":    "",
				"param":   nil,
				"code":    "",
			},
		},
		{
			name:        "error event with raw rate-limit message",
			line:        `{"type":"error","message":"exceeded retry limit, last status: 429 Too Many Requests, request id: req_123"}`,
			wantMatched: true,
			wantOK:      false,
			wantDetails: map[string]any{
				"message": "exceeded retry limit, last status: 429 Too Many Requests, request id: req_123",
				"type":    "",
				"param":   nil,
				"code":    "",
			},
		},
		{
			name:        "non-error event ignored",
			line:        `{"type":"result","message":"ok"}`,
			wantMatched: false,
			wantOK:      false,
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			normalized, details, matched, ok := normalizeProviderErrorLine([]byte(testCase.line))
			if matched != testCase.wantMatched {
				t.Fatalf("matched = %v, want %v", matched, testCase.wantMatched)
			}
			if ok != testCase.wantOK {
				t.Fatalf("ok = %v, want %v", ok, testCase.wantOK)
			}
			if !reflect.DeepEqual(details, testCase.wantDetails) {
				t.Fatalf("details = %#v, want %#v", details, testCase.wantDetails)
			}
			if !ok {
				return
			}

			var event map[string]any
			if err := json.Unmarshal(normalized, &event); err != nil {
				t.Fatalf("decode normalized: %v", err)
			}
			if got, _ := event["type"].(string); got != "error" {
				t.Fatalf("normalized type = %q, want error", got)
			}
			errorEnvelope, _ := event["error"].(map[string]any)
			if !reflect.DeepEqual(errorEnvelope, testCase.wantDetails) {
				t.Fatalf("normalized error = %#v, want %#v", errorEnvelope, testCase.wantDetails)
			}
		})
	}
}

func TestProviderErrorObserverRetainsLatestProviderErrorDetails(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		lines       []string
		wantDetails map[string]any
		wantStderr  string
	}{
		{
			name: "terminal rate limit replaces earlier reconnect error",
			lines: []string{
				`{"type":"error","message":"Reconnecting..."}`,
				`{"type":"error","message":"exceeded retry limit, last status: 429 Too Many Requests, request id: req_123"}`,
			},
			wantDetails: map[string]any{
				"message": "exceeded retry limit, last status: 429 Too Many Requests, request id: req_123",
				"type":    "",
				"param":   nil,
				"code":    "",
			},
			wantStderr: `{"type":"error","message":"Reconnecting..."}` + "\n" +
				`{"type":"error","message":"exceeded retry limit, last status: 429 Too Many Requests, request id: req_123"}` + "\n",
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var stdout bytes.Buffer
			var stderr bytes.Buffer
			observer := NewProviderErrorObserver(&stdout, &stderr)
			for _, line := range testCase.lines {
				if _, err := observer.Write([]byte(line + "\n")); err != nil {
					t.Fatalf("write observer line: %v", err)
				}
			}
			if err := observer.Close(); err != nil {
				t.Fatalf("close observer: %v", err)
			}

			if got := observer.ProviderErrorDetails(); !reflect.DeepEqual(got, testCase.wantDetails) {
				t.Fatalf("ProviderErrorDetails() = %#v, want %#v", got, testCase.wantDetails)
			}
			if got := stderr.String(); got != testCase.wantStderr {
				t.Fatalf("stderr = %q, want %q", got, testCase.wantStderr)
			}
		})
	}
}
