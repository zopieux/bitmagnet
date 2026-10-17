package classifier

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bitmagnet-io/bitmagnet/internal/classifier/classification"
	"github.com/bitmagnet-io/bitmagnet/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWebhookAction(t *testing.T) {
	t.Parallel()

	t.Run("successful keep with content type and tags", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
			assert.Equal(t, "secret", r.Header.Get("X-Api-Key"))

			var received model.Torrent
			err := json.NewDecoder(r.Body).Decode(&received)
			assert.NoError(t, err)
			assert.Equal(t, "Test Torrent", received.Name)

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(webhookVerdict{
				Action:       "keep",
				ContentType:  "movie",
				Tags:         []string{"verified", "hd"},
				ReleaseGroup: "TESTGRP",
			})
		}))
		defer ts.Close()

		c := compiler{
			options: []compilerOption{
				compilerFeatures(defaultFeatures),
				celEnvOption,
			},
		}

		runner, err := c.Compile(Source{
			Workflows: map[string]any{
				"default": []any{
					map[string]any{
						"webhook": map[string]any{
							"url": ts.URL,
							"headers": map[string]string{
								"X-Api-Key": "secret",
							},
						},
					},
				},
			},
		})
		require.NoError(t, err)

		res, err := runner.Run(context.Background(), "default", Flags{}, model.Torrent{
			Name:        "Test Torrent",
			FilesStatus: model.FilesStatusSingle,
			Hint: model.TorrentHint{
				ContentType: model.ContentTypeMovie,
			},
		})
		require.NoError(t, err)
		assert.Equal(t, model.ContentTypeMovie, res.ContentType.ContentType)
		assert.Equal(t, "TESTGRP", res.ReleaseGroup.String)
		assert.Contains(t, res.Tags, "verified")
		assert.Contains(t, res.Tags, "hd")
	})

	t.Run("verdict delete returns ErrDeleteTorrent", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(webhookVerdict{
				Action: "delete",
			})
		}))
		defer ts.Close()

		c := compiler{
			options: []compilerOption{
				compilerFeatures(defaultFeatures),
				celEnvOption,
			},
		}

		runner, err := c.Compile(Source{
			Workflows: map[string]any{
				"default": []any{
					map[string]any{
						"webhook": map[string]any{
							"url": ts.URL,
						},
					},
				},
			},
		})
		require.NoError(t, err)

		_, err = runner.Run(context.Background(), "default", Flags{}, model.Torrent{
			Name: "Spam Torrent",
		})
		require.ErrorIs(t, err, classification.ErrDeleteTorrent)
	})

	t.Run("verdict unmatched returns ErrUnmatched", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(webhookVerdict{
				Action: "unmatched",
			})
		}))
		defer ts.Close()

		c := compiler{
			options: []compilerOption{
				compilerFeatures(defaultFeatures),
				celEnvOption,
			},
		}

		runner, err := c.Compile(Source{
			Workflows: map[string]any{
				"default": []any{
					map[string]any{
						"webhook": map[string]any{
							"url": ts.URL,
						},
					},
				},
			},
		})
		require.NoError(t, err)

		_, err = runner.Run(context.Background(), "default", Flags{}, model.Torrent{
			Name: "Unknown Torrent",
		})
		require.ErrorIs(t, err, classification.ErrUnmatched)
	})

	t.Run("on_error retry returns error on 500", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer ts.Close()

		c := compiler{
			options: []compilerOption{
				compilerFeatures(defaultFeatures),
				celEnvOption,
			},
		}

		runner, err := c.Compile(Source{
			Workflows: map[string]any{
				"default": []any{
					map[string]any{
						"webhook": map[string]any{
							"url":      ts.URL,
							"on_error": "retry",
						},
					},
				},
			},
		})
		require.NoError(t, err)

		_, err = runner.Run(context.Background(), "default", Flags{}, model.Torrent{})
		require.Error(t, err)
		assert.NotErrorIs(t, err, classification.ErrDeleteTorrent)
	})

	t.Run("on_error ignore continues pipeline on failure", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
		}))
		defer ts.Close()

		c := compiler{
			options: []compilerOption{
				compilerFeatures(defaultFeatures),
				celEnvOption,
			},
		}

		runner, err := c.Compile(Source{
			Workflows: map[string]any{
				"default": []any{
					map[string]any{
						"webhook": map[string]any{
							"url":      ts.URL,
							"on_error": "ignore",
						},
					},
					map[string]any{
						"set_content_type": "music",
					},
				},
			},
		})
		require.NoError(t, err)

		res, err := runner.Run(context.Background(), "default", Flags{}, model.Torrent{})
		require.NoError(t, err)
		assert.Equal(t, model.ContentTypeMusic, res.ContentType.ContentType)
	})

	t.Run("on_error delete returns ErrDeleteTorrent on failure", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusGatewayTimeout)
		}))
		defer ts.Close()

		c := compiler{
			options: []compilerOption{
				compilerFeatures(defaultFeatures),
				celEnvOption,
			},
		}

		runner, err := c.Compile(Source{
			Workflows: map[string]any{
				"default": []any{
					map[string]any{
						"webhook": map[string]any{
							"url":      ts.URL,
							"on_error": "delete",
						},
					},
				},
			},
		})
		require.NoError(t, err)

		_, err = runner.Run(context.Background(), "default", Flags{}, model.Torrent{})
		require.ErrorIs(t, err, classification.ErrDeleteTorrent)
	})

	t.Run("timeout handling with on_error ignore", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(50 * time.Millisecond)
			w.WriteHeader(http.StatusOK)
		}))
		defer ts.Close()

		c := compiler{
			options: []compilerOption{
				compilerFeatures(defaultFeatures),
				celEnvOption,
			},
		}

		runner, err := c.Compile(Source{
			Workflows: map[string]any{
				"default": []any{
					map[string]any{
						"webhook": map[string]any{
							"url":      ts.URL,
							"timeout":  "10ms",
							"on_error": "ignore",
						},
					},
				},
			},
		})
		require.NoError(t, err)

		_, err = runner.Run(context.Background(), "default", Flags{}, model.Torrent{})
		require.NoError(t, err)
	})

	t.Run("unix domain socket support", func(t *testing.T) {
		socketDir := t.TempDir()
		socketPath := socketDir + "/test.sock"

		listener, err := net.Listen("unix", socketPath)
		require.NoError(t, err)
		defer listener.Close()

		server := &http.Server{
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/classify", r.URL.Path)
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(webhookVerdict{
					Action: "keep",
					Tags:   []string{"unix-ok"},
				})
			}),
		}
		go func() {
			_ = server.Serve(listener)
		}()
		defer server.Close()

		c := compiler{
			options: []compilerOption{
				compilerFeatures(defaultFeatures),
				celEnvOption,
			},
		}

		runner, err := c.Compile(Source{
			Workflows: map[string]any{
				"default": []any{
					map[string]any{
						"webhook": map[string]any{
							"url": "unix://" + socketPath + ":/classify",
						},
					},
				},
			},
		})
		require.NoError(t, err)

		res, err := runner.Run(context.Background(), "default", Flags{}, model.Torrent{
			Name: "Unix Test Torrent",
		})
		require.NoError(t, err)
		assert.Contains(t, res.Tags, "unix-ok")
	})
}
