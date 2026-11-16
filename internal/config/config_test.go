package config

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

const testJWTSecret = "0123456789abcdef0123456789abcdef"

func TestLoad_CookieDefaults(t *testing.T) {
	t.Setenv("JWT_SECRET_KEY", testJWTSecret)
	t.Setenv("COOKIE_SECURE", "")
	t.Setenv("COOKIE_SAMESITE", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if !cfg.CookieSecure {
		t.Errorf("CookieSecure default = false, want true")
	}
	if cfg.CookieSameSite != http.SameSiteLaxMode {
		t.Errorf("CookieSameSite default = %v, want Lax", cfg.CookieSameSite)
	}
}

func TestLoad_CookieSecure(t *testing.T) {
	t.Setenv("JWT_SECRET_KEY", testJWTSecret)
	t.Setenv("COOKIE_SAMESITE", "lax")

	cases := []struct {
		value string
		want  bool
	}{
		{"true", true},
		{"TRUE", true},
		{"1", true},
		{"false", false},
		{"FALSE", false},
		{"0", false},
	}
	for _, tc := range cases {
		t.Run(tc.value, func(t *testing.T) {
			t.Setenv("COOKIE_SECURE", tc.value)
			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load() unexpected error: %v", err)
			}
			if cfg.CookieSecure != tc.want {
				t.Errorf("CookieSecure = %v, want %v", cfg.CookieSecure, tc.want)
			}
		})
	}
}

func TestLoad_CookieSameSite(t *testing.T) {
	t.Setenv("JWT_SECRET_KEY", testJWTSecret)
	t.Setenv("COOKIE_SECURE", "true")

	cases := []struct {
		value string
		want  http.SameSite
	}{
		{"lax", http.SameSiteLaxMode},
		{"Lax", http.SameSiteLaxMode},
		{"strict", http.SameSiteStrictMode},
		{"STRICT", http.SameSiteStrictMode},
		{"none", http.SameSiteNoneMode},
		{"None", http.SameSiteNoneMode},
	}
	for _, tc := range cases {
		t.Run(tc.value, func(t *testing.T) {
			t.Setenv("COOKIE_SAMESITE", tc.value)
			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load() unexpected error: %v", err)
			}
			if cfg.CookieSameSite != tc.want {
				t.Errorf("CookieSameSite = %v, want %v", cfg.CookieSameSite, tc.want)
			}
		})
	}
}

func TestLoad_CookieSameSiteInvalid(t *testing.T) {
	t.Setenv("JWT_SECRET_KEY", testJWTSecret)
	t.Setenv("COOKIE_SAMESITE", "invalid")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want error for invalid COOKIE_SAMESITE")
	}
	if !strings.Contains(err.Error(), "COOKIE_SAMESITE") {
		t.Errorf("error %q does not mention COOKIE_SAMESITE", err)
	}
}

func TestLoad_SameSiteNoneRequiresSecure(t *testing.T) {
	t.Setenv("JWT_SECRET_KEY", testJWTSecret)
	t.Setenv("COOKIE_SECURE", "false")
	t.Setenv("COOKIE_SAMESITE", "none")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want error when SameSite=none and Secure=false")
	}
	if !strings.Contains(err.Error(), "COOKIE_SAMESITE=none") {
		t.Errorf("error %q does not explain the none/secure pairing", err)
	}
}

func TestLoad_PlainHTTPCombo(t *testing.T) {
	// The k8s ingress path: http://api.floxflix.lan with no TLS.
	t.Setenv("JWT_SECRET_KEY", testJWTSecret)
	t.Setenv("COOKIE_SECURE", "false")
	t.Setenv("COOKIE_SAMESITE", "lax")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if cfg.CookieSecure {
		t.Error("CookieSecure = true, want false for plain HTTP")
	}
	if cfg.CookieSameSite != http.SameSiteLaxMode {
		t.Errorf("CookieSameSite = %v, want Lax", cfg.CookieSameSite)
	}
}

func TestLoad_UIDefaults(t *testing.T) {
	t.Setenv("JWT_SECRET_KEY", testJWTSecret)
	t.Setenv("SERVE_UI", "")
	t.Setenv("UI_DIR", "")
	t.Setenv("VITE_STREAMAPI_URL", "")
	t.Setenv("VITE_STREAMAPI_PREFIX", "")
	t.Setenv("VITE_STREAMAPI_PREFIX_ADMIN", "")
	t.Setenv("VITE_DEFAULT_NEXT_EPISODE_OFFSET", "")
	t.Setenv("CORS_ORIGIN", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if !cfg.ServeUI {
		t.Error("ServeUI default = false, want true")
	}
	if cfg.UIStreamAPIURL != "" {
		t.Errorf("UIStreamAPIURL = %q, want empty (same origin)", cfg.UIStreamAPIURL)
	}
	if cfg.UIStreamAPIPrefix != "/v1/api" {
		t.Errorf("UIStreamAPIPrefix = %q, want /v1/api", cfg.UIStreamAPIPrefix)
	}
	if cfg.UIStreamAPIPrefixAdmin != "/dashboard" {
		t.Errorf("UIStreamAPIPrefixAdmin = %q, want /dashboard", cfg.UIStreamAPIPrefixAdmin)
	}
	if cfg.UIDefaultNextEpisodeOffset != "15" {
		t.Errorf("UIDefaultNextEpisodeOffset = %q, want 15", cfg.UIDefaultNextEpisodeOffset)
	}
	if len(cfg.CORSOrigins) != 0 {
		t.Errorf("CORSOrigins = %v, want empty", cfg.CORSOrigins)
	}
}

func TestLoad_CORSOrigins(t *testing.T) {
	t.Setenv("JWT_SECRET_KEY", testJWTSecret)
	t.Setenv("CORS_ORIGIN", " http://a.example , ,http://b.example ")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	want := []string{"http://a.example", "http://b.example"}
	if len(cfg.CORSOrigins) != len(want) {
		t.Fatalf("CORSOrigins = %v, want %v", cfg.CORSOrigins, want)
	}
	for i := range want {
		if cfg.CORSOrigins[i] != want[i] {
			t.Errorf("CORSOrigins[%d] = %q, want %q", i, cfg.CORSOrigins[i], want[i])
		}
	}
}

func TestParseSameSite(t *testing.T) {
	_, err := parseSameSite("  lax  ")
	if err != nil {
		t.Fatalf("parseSameSite(padded lax) error: %v", err)
	}
}

func TestLoad_TokenTTLDefaults(t *testing.T) {
	t.Setenv("JWT_SECRET_KEY", testJWTSecret)
	t.Setenv("ACCESS_TOKEN_TTL", "")
	t.Setenv("REFRESH_TOKEN_TTL", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if cfg.AccessTokenTTL != time.Hour {
		t.Errorf("AccessTokenTTL = %s, want 1h", cfg.AccessTokenTTL)
	}
	if cfg.RefreshTokenTTL != 24*time.Hour*30 {
		t.Errorf("RefreshTokenTTL = %s, want 720h", cfg.RefreshTokenTTL)
	}
}

func TestLoad_TokenTTLFromEnv(t *testing.T) {
	t.Setenv("JWT_SECRET_KEY", testJWTSecret)
	t.Setenv("ACCESS_TOKEN_TTL", "15m")
	t.Setenv("REFRESH_TOKEN_TTL", "2160h")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if cfg.AccessTokenTTL != 15*time.Minute {
		t.Errorf("AccessTokenTTL = %s, want 15m", cfg.AccessTokenTTL)
	}
	if cfg.RefreshTokenTTL != 2160*time.Hour {
		t.Errorf("RefreshTokenTTL = %s, want 2160h", cfg.RefreshTokenTTL)
	}
}

func TestLoad_TokenTTLInvalid(t *testing.T) {
	cases := []struct {
		key   string
		value string
	}{
		{"ACCESS_TOKEN_TTL", "30d"}, // Go durations use h, not d
		{"ACCESS_TOKEN_TTL", "1 hour"},
		{"ACCESS_TOKEN_TTL", "0s"},
		{"REFRESH_TOKEN_TTL", "-1h"},
	}

	for _, tc := range cases {
		t.Run(tc.key+"="+tc.value, func(t *testing.T) {
			t.Setenv("JWT_SECRET_KEY", testJWTSecret)
			t.Setenv(tc.key, tc.value)

			_, err := Load()
			if err == nil {
				t.Fatalf("Load() error = nil, want error for %s=%q", tc.key, tc.value)
			}
			if !strings.Contains(err.Error(), tc.key) {
				t.Errorf("error %q does not mention %s", err, tc.key)
			}
		})
	}
}

func TestLoad_RefreshTTLMustExceedAccess(t *testing.T) {
	t.Setenv("JWT_SECRET_KEY", testJWTSecret)
	t.Setenv("ACCESS_TOKEN_TTL", "2h")
	t.Setenv("REFRESH_TOKEN_TTL", "1h")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want error when refresh TTL <= access TTL")
	}
	if !strings.Contains(err.Error(), "REFRESH_TOKEN_TTL") {
		t.Errorf("error %q does not explain the refresh/access TTL relation", err)
	}
}
