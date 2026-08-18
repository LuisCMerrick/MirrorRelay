package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/LuisCMerrick/MirrorRelay/internal/model"
)

func TestWebhookDispatchAndHMACSignature(t *testing.T) {
	var mu sync.Mutex
	var received int
	var lastSignature string
	var lastEvent string
	var lastBody []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		sig := r.Header.Get("X-MirrorRelay-Signature")
		evt := r.Header.Get("X-MirrorRelay-Event")

		mu.Lock()
		received++
		lastSignature = sig
		lastEvent = evt
		lastBody = body
		mu.Unlock()

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	secret := "test-secret-key"
	d := New(model.WebhookConfig{
		Enabled: true,
		URL:     server.URL,
		Secret:  secret,
		Events:  []string{"upstream_status", "security_alert"},
		Timeout: 2 * time.Second,
	})
	defer d.Stop()

	// Dispatch enabled event
	d.Dispatch("upstream_status", "Upstream Degraded", "Upstream A is down", map[string]any{"repo": "debian"})

	// Wait for delivery
	time.Sleep(150 * time.Millisecond)

	mu.Lock()
	recCount := received
	sigVal := lastSignature
	evtVal := lastEvent
	bodyVal := make([]byte, len(lastBody))
	copy(bodyVal, lastBody)
	mu.Unlock()

	if recCount != 1 {
		t.Fatalf("expected 1 webhook delivered, got %d", recCount)
	}
	if evtVal != "upstream_status" {
		t.Fatalf("expected event upstream_status, got %q", evtVal)
	}

	// Verify HMAC signature
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(bodyVal)
	expectedSig := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if sigVal != expectedSig {
		t.Fatalf("signature mismatch: got %q, expected %q", sigVal, expectedSig)
	}

	// Test filtered-out event
	d.Dispatch("unconfigured_event", "Title", "Message", nil)
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	finalCount := received
	mu.Unlock()

	if finalCount != 1 {
		t.Fatalf("expected event to be filtered out, got %d", finalCount)
	}
}

func TestWebhookPlatformFormatting(t *testing.T) {
	payload := model.WebhookPayload{
		Event:     "security_alert",
		Timestamp: time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC),
		Title:     "Package Blocked",
		Message:   "Blocked malicious-package-1.0.tar.gz",
	}

	// Test DingTalk
	dingBytes, _ := formatPayload("https://oapi.dingtalk.com/robot/send?access_token=xxx", payload)
	var dingMap map[string]any
	if err := json.Unmarshal(dingBytes, &dingMap); err != nil || dingMap["msgtype"] != "markdown" {
		t.Fatalf("dingtalk format error: %v, map=%v", err, dingMap)
	}

	// Test Feishu
	feishuBytes, _ := formatPayload("https://open.feishu.cn/open-apis/bot/v2/hook/xxx", payload)
	var feishuMap map[string]any
	if err := json.Unmarshal(feishuBytes, &feishuMap); err != nil || feishuMap["msg_type"] != "post" {
		t.Fatalf("feishu format error: %v, map=%v", err, feishuMap)
	}

	// Test WeCom
	wecomBytes, _ := formatPayload("https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=xxx", payload)
	var wecomMap map[string]any
	if err := json.Unmarshal(wecomBytes, &wecomMap); err != nil || wecomMap["msgtype"] != "markdown" {
		t.Fatalf("wecom format error: %v, map=%v", err, wecomMap)
	}

	// Test Slack
	slackBytes, _ := formatPayload("https://hooks.slack.com/services/xxx", payload)
	var slackMap map[string]any
	if err := json.Unmarshal(slackBytes, &slackMap); err != nil || slackMap["text"] == nil {
		t.Fatalf("slack format error: %v, map=%v", err, slackMap)
	}
}

func TestWebhookSendSync(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	d := New(model.WebhookConfig{
		Enabled: true,
		URL:     server.URL,
		Timeout: 2 * time.Second,
	})
	defer d.Stop()

	err := d.SendSync(context.Background(), model.WebhookPayload{
		Event:     "test",
		Timestamp: time.Now(),
		Title:     "Test Webhook",
		Message:   "This is a test notification",
	})
	if err != nil {
		t.Fatalf("SendSync failed: %v", err)
	}
}
