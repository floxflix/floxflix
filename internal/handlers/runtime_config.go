package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/floxflix/floxflix/internal/config"
	"github.com/go-chi/chi/v5"
)

type runtimeConfig struct {
	VITEStreamAPIURL             string `json:"VITE_STREAMAPI_URL"`
	VITEStreamAPIPrefix          string `json:"VITE_STREAMAPI_PREFIX"`
	VITEStreamAPIPrefixAdmin     string `json:"VITE_STREAMAPI_PREFIX_ADMIN"`
	VITEDefaultNextEpisodeOffset string `json:"VITE_DEFAULT_NEXT_EPISODE_OFFSET"`
}

// RegisterRuntimeConfig serves the SPA's runtime configuration. The UI reads
// window._env_ at boot, which means one built image can be reconfigured per
// deployment without rebuilding the frontend. An empty VITE_STREAMAPI_URL
// keeps the UI on the same origin.
func RegisterRuntimeConfig(r chi.Router, cfg *config.Config) {
	payload, err := json.MarshalIndent(runtimeConfig{
		VITEStreamAPIURL:             cfg.UIStreamAPIURL,
		VITEStreamAPIPrefix:          cfg.UIStreamAPIPrefix,
		VITEStreamAPIPrefixAdmin:     cfg.UIStreamAPIPrefixAdmin,
		VITEDefaultNextEpisodeOffset: cfg.UIDefaultNextEpisodeOffset,
	}, "", "  ")
	if err != nil {
		panic(err)
	}
	body := fmt.Appendf(nil, "window._env_ = %s;\n", payload)

	r.Get("/config.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Write(body)
	})
}
