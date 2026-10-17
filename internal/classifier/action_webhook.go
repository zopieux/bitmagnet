package classifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/bitmagnet-io/bitmagnet/internal/classifier/classification"
	"github.com/bitmagnet-io/bitmagnet/internal/model"
)

const webhookActionName = "webhook"

type webhookAction struct{}

func (webhookAction) name() string {
	return webhookActionName
}

type webhookConfig struct {
	URL     string            `mapstructure:"url"`
	Timeout string            `mapstructure:"timeout"`
	Headers map[string]string `mapstructure:"headers"`
	OnError string            `mapstructure:"on_error"`
}

type webhookVerdict struct {
	Action       string   `json:"action"` // "keep" (or ""), "delete", "unmatched"
	ContentType  string   `json:"contentType"`
	Tags         []string `json:"tags"`
	ReleaseGroup string   `json:"releaseGroup"`
}

var webhookPayloadSpec = payloadSingleKeyValue[webhookConfig]{
	key: webhookActionName,
	valueSpec: payloadMustSucceed[webhookConfig]{
		payload: payloadStruct[webhookConfig]{
			jsonSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"url": map[string]any{
						"type":      "string",
						"minLength": 1,
					},
					"timeout": map[string]any{
						"type": "string",
					},
					"headers": map[string]any{
						"type": "object",
						"additionalProperties": map[string]any{
							"type": "string",
						},
					},
					"on_error": map[string]any{
						"type": "string",
						"enum": []string{"retry", "ignore", "delete"},
					},
				},
				"required":             []string{"url"},
				"additionalProperties": false,
			},
		},
	},
	description: "Call an external HTTP endpoint with torrent metadata to determine classification verdict",
}

func (webhookAction) JSONSchema() JSONSchema {
	return webhookPayloadSpec.JSONSchema()
}

func (webhookAction) compileAction(ctx compilerContext) (action, error) {
	cfg, err := webhookPayloadSpec.Unmarshal(ctx)
	if err != nil {
		return action{}, ctx.error(err)
	}

	timeout := 10 * time.Second
	if cfg.Timeout != "" {
		d, parseErr := time.ParseDuration(cfg.Timeout)
		if parseErr != nil {
			return action{}, ctx.error(fmt.Errorf("invalid timeout duration '%s': %w", cfg.Timeout, parseErr))
		}
		timeout = d
	}

	onError := strings.ToLower(cfg.OnError)
	if onError == "" {
		onError = "retry"
	}
	if onError != "retry" && onError != "ignore" && onError != "delete" {
		return action{}, ctx.error(fmt.Errorf("invalid on_error '%s', must be 'retry', 'ignore', or 'delete'", cfg.OnError))
	}

	reqURL := cfg.URL
	var transport http.RoundTripper

	if strings.HasPrefix(cfg.URL, "unix://") {
		// unix:///path/to/socket.sock or unix:///path/to/socket.sock:/path
		raw := strings.TrimPrefix(cfg.URL, "unix://")
		var socketPath, requestPath string
		if colonIdx := strings.Index(raw, ":"); colonIdx != -1 {
			socketPath = raw[:colonIdx]
			requestPath = raw[colonIdx+1:]
		} else {
			socketPath = raw
			requestPath = "/"
		}
		if !strings.HasPrefix(requestPath, "/") {
			requestPath = "/" + requestPath
		}
		reqURL = "http://unix" + requestPath

		transport = &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", socketPath)
			},
		}
	}

	httpClient := &http.Client{
		Timeout:   timeout,
		Transport: transport,
	}

	return action{
		run: func(ec executionContext) (classification.Result, error) {
			handleErr := func(cause error) (classification.Result, error) {
				switch onError {
				case "ignore":
					return ec.result, nil
				case "delete":
					return classification.Result{}, classification.ErrDeleteTorrent
				default:
					return classification.Result{}, fmt.Errorf("webhook error: %w", cause)
				}
			}

			payloadBytes, err := json.Marshal(ec.torrent)
			if err != nil {
				return handleErr(fmt.Errorf("failed to encode torrent JSON: %w", err))
			}

			req, err := http.NewRequestWithContext(
				ec.Context,
				http.MethodPost,
				reqURL,
				bytes.NewReader(payloadBytes),
			)
			if err != nil {
				return handleErr(fmt.Errorf("failed to create http request: %w", err))
			}

			req.Header.Set("Content-Type", "application/json")
			for k, v := range cfg.Headers {
				req.Header.Set(k, v)
			}

			resp, err := httpClient.Do(req)
			if err != nil {
				return handleErr(err)
			}
			defer resp.Body.Close()

			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
				return handleErr(fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(bodyBytes)))
			}

			var verdict webhookVerdict
			if err := json.NewDecoder(resp.Body).Decode(&verdict); err != nil {
				return handleErr(fmt.Errorf("failed to decode response JSON: %w", err))
			}

			switch strings.ToLower(verdict.Action) {
			case "delete":
				return classification.Result{}, classification.ErrDeleteTorrent
			case "unmatched":
				return classification.Result{}, classification.ErrUnmatched
			}

			result := ec.result
			if verdict.ContentType != "" && verdict.ContentType != "unknown" {
				ct, parseErr := model.ParseContentType(verdict.ContentType)
				if parseErr == nil {
					result.ContentType = model.NewNullContentType(ct)
				}
			}

			if verdict.ReleaseGroup != "" {
				result.ReleaseGroup = model.NewNullString(verdict.ReleaseGroup)
			}

			if len(verdict.Tags) > 0 {
				if result.Tags == nil {
					result.Tags = make(map[string]struct{}, len(verdict.Tags))
				}
				for _, tag := range verdict.Tags {
					result.Tags[tag] = struct{}{}
				}
			}

			return result, nil
		},
	}, nil
}
