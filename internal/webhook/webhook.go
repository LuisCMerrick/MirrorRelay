// Package webhook provides asynchronous webhook alert and notification dispatching.
package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/LuisCMerrick/MirrorRelay/internal/model"
	"github.com/LuisCMerrick/MirrorRelay/internal/security"
)

// Dispatcher manages sending event notifications to external webhook endpoints.
type Dispatcher struct {
	mu    sync.RWMutex
	cfg   model.WebhookConfig
	queue chan model.WebhookPayload
	stop  chan struct{}
	wg    sync.WaitGroup
}

// New creates and starts a new Webhook Dispatcher.
func New(cfg model.WebhookConfig) *Dispatcher {
	d := &Dispatcher{
		cfg:   cfg,
		queue: make(chan model.WebhookPayload, 256),
		stop:  make(chan struct{}),
	}
	d.wg.Add(1)
	go d.worker()
	return d
}

// UpdateConfig updates the dispatcher's configuration dynamically.
func (d *Dispatcher) UpdateConfig(cfg model.WebhookConfig) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.cfg = cfg
}

// Stop stops the background worker.
func (d *Dispatcher) Stop() {
	close(d.stop)
	d.wg.Wait()
}

// Dispatch queues a webhook event if the event type is enabled.
func (d *Dispatcher) Dispatch(event, title, message string, data map[string]any) {
	d.mu.RLock()
	cfg := d.cfg
	d.mu.RUnlock()

	if !cfg.Enabled || cfg.URL == "" {
		return
	}
	if !d.isEventEnabled(cfg, event) {
		return
	}

	payload := model.WebhookPayload{
		Event:     event,
		Timestamp: time.Now().UTC(),
		Title:     title,
		Message:   message,
		Data:      data,
	}

	select {
	case d.queue <- payload:
	default:
		slog.Warn("webhook queue full, dropping alert", "event", event, "title", title)
	}
}

// SendSync sends a webhook notification synchronously, useful for manual test triggers.
func (d *Dispatcher) SendSync(ctx context.Context, payload model.WebhookPayload) error {
	d.mu.RLock()
	cfg := d.cfg
	d.mu.RUnlock()

	if cfg.URL == "" {
		return fmt.Errorf("webhook URL is empty")
	}
	return d.postPayload(ctx, cfg, payload)
}

// SendSyncWithConfig sends a test notification with an explicitly validated
// temporary configuration without mutating the dispatcher's live settings.
func (d *Dispatcher) SendSyncWithConfig(ctx context.Context, cfg model.WebhookConfig, payload model.WebhookPayload) error {
	if cfg.URL == "" {
		return fmt.Errorf("webhook URL is empty")
	}
	return d.postPayload(ctx, cfg, payload)
}

func (d *Dispatcher) isEventEnabled(cfg model.WebhookConfig, event string) bool {
	if len(cfg.Events) == 0 {
		return true
	}
	for _, e := range cfg.Events {
		if strings.EqualFold(e, event) || e == "*" || e == "all" {
			return true
		}
	}
	return false
}

func (d *Dispatcher) worker() {
	defer d.wg.Done()
	for {
		select {
		case <-d.stop:
			return
		case payload := <-d.queue:
			d.mu.RLock()
			cfg := d.cfg
			d.mu.RUnlock()
			if !cfg.Enabled || cfg.URL == "" {
				continue
			}
			ctx, cancel := context.WithTimeout(context.Background(), webhookTimeout(cfg))
			if err := d.postPayload(ctx, cfg, payload); err != nil {
				slog.Warn("failed to deliver webhook", "event", payload.Event, "error", err)
			}
			cancel()
		}
	}
}

func (d *Dispatcher) postPayload(ctx context.Context, cfg model.WebhookConfig, payload model.WebhookPayload) error {
	if err := security.ValidateOutboundURLSyntax(cfg.URL, cfg.AllowHTTP); err != nil {
		return fmt.Errorf("validate webhook URL: %w", err)
	}
	if err := security.ValidateResolvedURL(ctx, cfg.URL, cfg.AllowHTTP, cfg.AllowPrivate, net.DefaultResolver); err != nil {
		return fmt.Errorf("validate webhook URL: %w", err)
	}
	body, contentType := formatPayload(cfg.URL, payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.URL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create webhook request: %w", err)
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("User-Agent", "MirrorRelay-Webhook/1.0")
	req.Header.Set("X-MirrorRelay-Event", payload.Event)

	if cfg.Secret != "" {
		mac := hmac.New(sha256.New, []byte(cfg.Secret))
		mac.Write(body)
		signature := hex.EncodeToString(mac.Sum(nil))
		req.Header.Set("X-MirrorRelay-Signature", "sha256="+signature)
	}

	resp, err := secureWebhookClient(cfg).Do(req)
	if err != nil {
		return fmt.Errorf("send webhook: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook responded with status %d", resp.StatusCode)
	}
	return nil
}

func webhookTimeout(cfg model.WebhookConfig) time.Duration {
	if cfg.Timeout > 0 {
		return cfg.Timeout
	}
	return 5 * time.Second
}

func secureWebhookClient(cfg model.WebhookConfig) *http.Client {
	timeout := webhookTimeout(cfg)
	dialer := security.NewSafeDialer(timeout, timeout, cfg.AllowPrivate)
	transport := &http.Transport{
		DialContext:           dialer.DialContext,
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: false},
		TLSHandshakeTimeout:   timeout,
		ResponseHeaderTimeout: timeout,
		DisableKeepAlives:     true,
		MaxIdleConns:          8,
		MaxIdleConnsPerHost:   2,
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("webhook redirect limit exceeded")
			}
			if err := security.ValidateOutboundURLSyntax(request.URL.String(), cfg.AllowHTTP); err != nil {
				return err
			}
			return security.ValidateResolvedURL(request.Context(), request.URL.String(), cfg.AllowHTTP, cfg.AllowPrivate, net.DefaultResolver)
		},
	}
}

func formatPayload(targetURL string, payload model.WebhookPayload) ([]byte, string) {
	hostname := ""
	if parsed, err := url.Parse(targetURL); err == nil {
		hostname = strings.ToLower(parsed.Hostname())
	}
	switch {
	case hostname == "oapi.dingtalk.com":
		// DingTalk bot markdown format
		text := fmt.Sprintf("### %s\n\n%s\n\n> Event: `%s` | Time: %s",
			payload.Title, payload.Message, payload.Event, payload.Timestamp.Format(time.RFC3339))
		msg := map[string]any{
			"msgtype": "markdown",
			"markdown": map[string]string{
				"title": payload.Title,
				"text":  text,
			},
		}
		b, _ := json.Marshal(msg)
		return b, "application/json"

	case hostname == "open.feishu.cn":
		// Feishu / Lark bot post format
		content := [][]map[string]string{
			{{"tag": "text", "text": payload.Message + "\n"}},
			{{"tag": "text", "text": "Event: " + payload.Event + " | Time: " + payload.Timestamp.Format(time.RFC3339)}},
		}
		msg := map[string]any{
			"msg_type": "post",
			"content": map[string]any{
				"post": map[string]any{
					"zh_cn": map[string]any{
						"title":   "【MirrorRelay】" + payload.Title,
						"content": content,
					},
				},
			},
		}
		b, _ := json.Marshal(msg)
		return b, "application/json"

	case hostname == "qyapi.weixin.qq.com":
		// Enterprise WeChat / WeCom markdown format
		text := fmt.Sprintf("### %s\n%s\n> **Event**: <font color=\"info\">%s</font>\n> **Time**: %s",
			payload.Title, payload.Message, payload.Event, payload.Timestamp.Format(time.RFC3339))
		msg := map[string]any{
			"msgtype": "markdown",
			"markdown": map[string]string{
				"content": text,
			},
		}
		b, _ := json.Marshal(msg)
		return b, "application/json"

	case hostname == "hooks.slack.com":
		// Slack webhook format
		msg := map[string]any{
			"text": fmt.Sprintf("*%s*\n%s\n`Event: %s`", payload.Title, payload.Message, payload.Event),
		}
		b, _ := json.Marshal(msg)
		return b, "application/json"

	default:
		// Standard MirrorRelay JSON format
		b, _ := json.Marshal(payload)
		return b, "application/json"
	}
}
