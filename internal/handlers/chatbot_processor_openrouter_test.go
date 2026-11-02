package handlers

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/shridarpatil/whatomate/test/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func openRouterSettings(model, orgKey string) *models.ChatbotSettings {
	return &models.ChatbotSettings{
		AI: models.AIConfig{
			Enabled:     true,
			Provider:    models.AIProviderOpenRouter,
			APIKey:      orgKey,
			Model:       model,
			MaxTokens:   128,
			Temperature: 0.7,
		},
	}
}

func TestGenerateOpenRouterResponse(t *testing.T) {
	origURL := openRouterChatCompletionsURL
	t.Cleanup(func() { openRouterChatCompletionsURL = origURL })
	t.Setenv(openRouterAPIKeyEnv, "")

	t.Run("sends model and bearer token and trims content", func(t *testing.T) {
		var calls atomic.Int32
		var gotAuth, gotModel, gotType string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			gotAuth = r.Header.Get("Authorization")
			gotType = r.Header.Get("Content-Type")
			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			require.Contains(t, string(body), `"model":"openai/gpt-6-luna"`)
			gotModel = "openai/gpt-6-luna"
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"  hello world  "}}]}`))
		}))
		defer server.Close()
		openRouterChatCompletionsURL = server.URL

		app := &App{Log: testutil.NopLogger(), HTTPClient: server.Client()}
		text, err := app.generateOpenRouterResponse(openRouterSettings("openai/gpt-6-luna", "org-secret"), nil, "hi", "")
		require.NoError(t, err)
		assert.Equal(t, "hello world", text)
		assert.Equal(t, "Bearer org-secret", gotAuth)
		assert.Equal(t, "application/json", gotType)
		assert.Equal(t, "openai/gpt-6-luna", gotModel)
		assert.Equal(t, int32(1), calls.Load())
	})

	t.Run("organization key overrides platform key", func(t *testing.T) {
		t.Setenv(openRouterAPIKeyEnv, "platform-secret")
		var gotAuth string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotAuth = r.Header.Get("Authorization")
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
		}))
		defer server.Close()
		openRouterChatCompletionsURL = server.URL

		app := &App{Log: testutil.NopLogger(), HTTPClient: server.Client()}
		_, err := app.generateOpenRouterResponse(openRouterSettings("z-ai/glm-5.3-flash", "org-secret"), nil, "hi", "")
		require.NoError(t, err)
		assert.Equal(t, "Bearer org-secret", gotAuth)
		assert.NotContains(t, gotAuth, "platform-secret")
	})

	t.Run("platform key is used when organization key is empty", func(t *testing.T) {
		t.Setenv(openRouterAPIKeyEnv, "platform-secret")
		var gotAuth string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotAuth = r.Header.Get("Authorization")
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
		}))
		defer server.Close()
		openRouterChatCompletionsURL = server.URL

		app := &App{Log: testutil.NopLogger(), HTTPClient: server.Client()}
		text, err := app.generateOpenRouterResponse(openRouterSettings("deepseek/deepseek-v4-flash-0731", ""), nil, "hi", "")
		require.NoError(t, err)
		assert.Equal(t, "ok", text)
		assert.Equal(t, "Bearer platform-secret", gotAuth)
	})

	t.Run("missing both keys is a configuration error", func(t *testing.T) {
		t.Setenv(openRouterAPIKeyEnv, "")
		var calls atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
		}))
		defer server.Close()
		openRouterChatCompletionsURL = server.URL

		app := &App{Log: testutil.NopLogger(), HTTPClient: server.Client()}
		_, err := app.generateOpenRouterResponse(openRouterSettings("z-ai/glm-5.3-flash", ""), nil, "hi", "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "OpenRouter API key is not configured")
		assert.Equal(t, int32(0), calls.Load())
	})

	t.Run("non-200 returns provider error without retry", func(t *testing.T) {
		var calls atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"bad model"}}`))
		}))
		defer server.Close()
		openRouterChatCompletionsURL = server.URL

		app := &App{Log: testutil.NopLogger(), HTTPClient: server.Client()}
		_, err := app.generateOpenRouterResponse(openRouterSettings("openai/gpt-6-luna", "org-secret"), nil, "hi", "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "bad model")
		assert.Equal(t, int32(1), calls.Load())
	})

	t.Run("malformed JSON is handled", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"choices":`))
		}))
		defer server.Close()
		openRouterChatCompletionsURL = server.URL

		app := &App{Log: testutil.NopLogger(), HTTPClient: server.Client()}
		_, err := app.generateOpenRouterResponse(openRouterSettings("openai/gpt-6-luna", "org-secret"), nil, "hi", "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse response")
	})
}

func TestGenerateAIResponse_ExistingProvidersUnchanged(t *testing.T) {
	type captured struct {
		url   string
		auth  string
		calls int
	}
	clientFor := func(body string, cap *captured) *http.Client {
		return &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			cap.calls++
			cap.url = r.URL.Scheme + "://" + r.URL.Host + r.URL.Path
			cap.auth = r.Header.Get("Authorization")
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(body)),
				Header:     make(http.Header),
				Request:    r,
			}, nil
		})}
	}

	settings := func(provider models.AIProvider, key, model string) *models.ChatbotSettings {
		return &models.ChatbotSettings{AI: models.AIConfig{Provider: provider, APIKey: key, Model: model, MaxTokens: 10}}
	}

	t.Run("openai", func(t *testing.T) {
		cap := &captured{}
		app := &App{Log: testutil.NopLogger(), HTTPClient: clientFor(`{"choices":[{"message":{"content":"openai-ok"}}]}`, cap)}
		text, err := app.generateOpenAIResponse(settings(models.AIProviderOpenAI, "openai-key", "gpt-4o"), nil, "hi", "")
		require.NoError(t, err)
		assert.Equal(t, "openai-ok", text)
		assert.Equal(t, "https://api.openai.com/v1/chat/completions", cap.url)
		assert.Equal(t, "Bearer openai-key", cap.auth)
		assert.Equal(t, 1, cap.calls)
	})

	t.Run("anthropic", func(t *testing.T) {
		cap := &captured{}
		app := &App{Log: testutil.NopLogger(), HTTPClient: clientFor(`{"content":[{"type":"text","text":"anthropic-ok"}]}`, cap)}
		text, err := app.generateAnthropicResponse(settings(models.AIProviderAnthropic, "anthropic-key", "claude-3-5-haiku-latest"), nil, "hi", "")
		require.NoError(t, err)
		assert.Equal(t, "anthropic-ok", text)
		assert.Equal(t, "https://api.anthropic.com/v1/messages", cap.url)
		assert.Equal(t, 1, cap.calls)
		assert.Empty(t, cap.auth)
	})

	t.Run("google", func(t *testing.T) {
		cap := &captured{}
		app := &App{Log: testutil.NopLogger(), HTTPClient: clientFor(`{"candidates":[{"content":{"parts":[{"text":"google-ok"}]}}]}`, cap)}
		text, err := app.generateGoogleResponse(settings(models.AIProviderGoogle, "google-key", "gemini-2.0-flash"), nil, "hi", "")
		require.NoError(t, err)
		assert.Equal(t, "google-ok", text)
		assert.Equal(t, "https://generativelanguage.googleapis.com/v1beta/models/gemini-2.0-flash:generateContent", cap.url)
		assert.Equal(t, 1, cap.calls)
	})
}

func TestAICredentialsReady_OpenRouterPlatformKey(t *testing.T) {
	t.Setenv(openRouterAPIKeyEnv, "platform-secret")
	app := &App{}
	ready := app.aiCredentialsReady(&models.ChatbotSettings{AI: models.AIConfig{
		Enabled: true, Provider: models.AIProviderOpenRouter,
	}})
	assert.True(t, ready)

	notReady := app.aiCredentialsReady(&models.ChatbotSettings{AI: models.AIConfig{
		Enabled: true, Provider: models.AIProviderOpenAI,
	}})
	assert.False(t, notReady)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
