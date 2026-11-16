package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/floxflix/floxflix/internal/config"
)

func TestNewAuthCookie_UsesConfigFlags(t *testing.T) {
	cases := []struct {
		name     string
		secure   bool
		sameSite http.SameSite
		want     []string
		notWant  []string
	}{
		{
			name:     "https defaults",
			secure:   true,
			sameSite: http.SameSiteLaxMode,
			want:     []string{"HttpOnly", "Secure", "SameSite=Lax", "Path=/"},
		},
		{
			name:     "plain http lan",
			secure:   false,
			sameSite: http.SameSiteLaxMode,
			want:     []string{"HttpOnly", "SameSite=Lax", "Path=/"},
			notWant:  []string{"Secure"},
		},
		{
			name:     "cross site https",
			secure:   true,
			sameSite: http.SameSiteNoneMode,
			want:     []string{"HttpOnly", "Secure", "SameSite=None"},
		},
		{
			name:     "strict",
			secure:   true,
			sameSite: http.SameSiteStrictMode,
			want:     []string{"SameSite=Strict", "Secure"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{CookieSecure: tc.secure, CookieSameSite: tc.sameSite}
			rec := httptest.NewRecorder()
			http.SetCookie(rec, newAuthCookie(cfg, "access_token", "tok", 3600))

			got := rec.Header().Get("Set-Cookie")
			if got == "" {
				t.Fatal("missing Set-Cookie header")
			}
			for _, s := range tc.want {
				if !strings.Contains(got, s) {
					t.Errorf("Set-Cookie %q missing %q", got, s)
				}
			}
			for _, s := range tc.notWant {
				if cookieHeaderHasAttr(got, s) {
					t.Errorf("Set-Cookie %q unexpectedly contains %q", got, s)
				}
			}
			if !strings.HasPrefix(got, "access_token=tok") {
				t.Errorf("Set-Cookie %q does not start with name=value", got)
			}
			if !strings.Contains(got, "Max-Age=3600") {
				t.Errorf("Set-Cookie %q missing Max-Age=3600", got)
			}
		})
	}
}

func TestNewAuthCookie_LogoutClears(t *testing.T) {
	cfg := &config.Config{CookieSecure: false, CookieSameSite: http.SameSiteLaxMode}
	rec := httptest.NewRecorder()
	http.SetCookie(rec, newAuthCookie(cfg, "refresh_token", "", -1))

	got := rec.Header().Get("Set-Cookie")
	if !strings.Contains(got, "Max-Age=0") && !strings.Contains(got, "Max-Age=-1") {
		t.Errorf("logout cookie %q should expire immediately", got)
	}
	if cookieHeaderHasAttr(got, "Secure") {
		t.Errorf("plain-HTTP logout cookie %q still has Secure", got)
	}
}

// cookieHeaderHasAttr reports whether Set-Cookie contains the attribute as a
// distinct flag (so "Secure" does not match "SameSite=Lax" etc.).
func cookieHeaderHasAttr(header, attr string) bool {
	for _, part := range strings.Split(header, ";") {
		if strings.EqualFold(strings.TrimSpace(part), attr) {
			return true
		}
	}
	return false
}

func TestSetAuthCookies_WritesBothTokens(t *testing.T) {
	cfg := &config.Config{
		AccessTokenTTL:  15 * time.Minute,
		RefreshTokenTTL: 24 * time.Hour,
		CookieSecure:    true,
		CookieSameSite:  http.SameSiteLaxMode,
	}
	rec := httptest.NewRecorder()

	setAuthCookies(rec, cfg, "access-tok", "refresh-tok")

	got := map[string]*http.Cookie{}
	for _, c := range rec.Result().Cookies() {
		got[c.Name] = c
	}

	access, ok := got["access_token"]
	if !ok {
		t.Fatal("missing access_token cookie")
	}
	if access.Value != "access-tok" {
		t.Errorf("access_token = %q, want access-tok", access.Value)
	}
	if access.MaxAge != 900 {
		t.Errorf("access_token MaxAge = %d, want 900", access.MaxAge)
	}

	refresh, ok := got["refresh_token"]
	if !ok {
		t.Fatal("missing refresh_token cookie")
	}
	if refresh.Value != "refresh-tok" {
		t.Errorf("refresh_token = %q, want refresh-tok", refresh.Value)
	}
	if refresh.MaxAge != 86400 {
		t.Errorf("refresh_token MaxAge = %d, want 86400", refresh.MaxAge)
	}
	if !refresh.HttpOnly || !refresh.Secure {
		t.Errorf("refresh_token flags HttpOnly=%v Secure=%v, want both true", refresh.HttpOnly, refresh.Secure)
	}
}
