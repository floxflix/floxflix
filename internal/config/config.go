package config

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const minJWTSecretLen = 32

type Config struct {
	MongoURI       string
	JWTSecret      []byte
	CORSOrigins    []string
	UploadFolder   string
	Port           string
	MaxFileSize    int64
	HLSSegmentTime int
	HLSListSize    int
	HLSSegmentType string

	AccessTokenTTL  time.Duration
	RefreshTokenTTL time.Duration

	// CookieSecure and CookieSameSite control auth cookies set by the
	// login/refresh/logout handlers. Defaults (true / Lax) are correct for
	// HTTPS. Plain-HTTP deploys must set COOKIE_SECURE=false.
	CookieSecure   bool
	CookieSameSite http.SameSite

	// UI configuration for the all-in-one image.
	ServeUI bool
	// UIDir, when set, serves the SPA from this directory instead of the
	// embedded build. Intended for local UI iteration without rebuilding.
	UIDir string
	// The values below are emitted by /config.js and read by the SPA at
	// runtime. Empty UIStreamAPIURL means same origin.
	UIStreamAPIURL             string
	UIStreamAPIPrefix          string
	UIStreamAPIPrefixAdmin     string
	UIDefaultNextEpisodeOffset string
}

func Load() (*Config, error) {
	jwtKey := os.Getenv("JWT_SECRET_KEY")
	if jwtKey == "" {
		return nil, fmt.Errorf("JWT_SECRET_KEY environment variable must be set")
	}
	if len(jwtKey) < minJWTSecretLen {
		return nil, fmt.Errorf("JWT_SECRET_KEY must be at least %d bytes (got %d); generate one with: openssl rand -base64 48", minJWTSecretLen, len(jwtKey))
	}

	cookieSecure := getEnvBool("COOKIE_SECURE", true)
	cookieSameSite, err := parseSameSite(getEnv("COOKIE_SAMESITE", "lax"))
	if err != nil {
		return nil, err
	}
	if cookieSameSite == http.SameSiteNoneMode && !cookieSecure {
		return nil, fmt.Errorf("COOKIE_SAMESITE=none requires COOKIE_SECURE=true; browsers reject SameSite=None on non-secure cookies")
	}

	return &Config{
		MongoURI:        getEnv("MONGO_URI", "mongodb://localhost:27017/floxflix"),
		JWTSecret:       []byte(jwtKey),
		CORSOrigins:     splitCSV(getEnv("CORS_ORIGIN", "")),
		UploadFolder:    getEnv("UPLOAD_FOLDER", "./uploads"),
		Port:            getEnv("PORT", "7777"),
		MaxFileSize:     getEnvInt64("MAX_FILE_SIZE", 2<<30), // 2GB
		HLSSegmentTime:  getEnvInt("HLS_SEGMENT_TIME", 10),
		HLSListSize:     getEnvInt("HLS_LIST_SIZE", 0),
		HLSSegmentType:  getEnv("HLS_SEGMENT_TYPE", "fmp4"),
		AccessTokenTTL:  time.Hour * 1,
		RefreshTokenTTL: time.Hour * 24 * 30,
		CookieSecure:    cookieSecure,
		CookieSameSite:  cookieSameSite,

		ServeUI:                    getEnvBool("SERVE_UI", true),
		UIDir:                      os.Getenv("UI_DIR"),
		UIStreamAPIURL:             os.Getenv("VITE_STREAMAPI_URL"),
		UIStreamAPIPrefix:          getEnv("VITE_STREAMAPI_PREFIX", "/v1/api"),
		UIStreamAPIPrefixAdmin:     getEnv("VITE_STREAMAPI_PREFIX_ADMIN", "/dashboard"),
		UIDefaultNextEpisodeOffset: getEnv("VITE_DEFAULT_NEXT_EPISODE_OFFSET", "15"),
	}, nil
}

// splitCSV trims each entry and drops empties, so an unset CORS_ORIGIN
// disables the middleware instead of matching the empty Origin header.
func splitCSV(v string) []string {
	var out []string
	for _, part := range strings.Split(v, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func getEnvInt64(key string, fallback int64) int64 {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return fallback
}

func parseSameSite(v string) (http.SameSite, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "lax":
		return http.SameSiteLaxMode, nil
	case "strict":
		return http.SameSiteStrictMode, nil
	case "none":
		return http.SameSiteNoneMode, nil
	default:
		return 0, fmt.Errorf("COOKIE_SAMESITE must be one of lax, strict, none (got %q)", v)
	}
}
