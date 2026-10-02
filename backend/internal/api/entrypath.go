package api

import (
	"encoding/json"
	"net/http"

	"clicd/internal/config"
)

type entryPathSettingsResponse struct {
	EntryPath string `json:"entry_path"`
	Enabled   bool   `json:"enabled"`
}

func entryPathSettingsStatus() entryPathSettingsResponse {
	prefix := config.AppConfig.EntryPath
	return entryPathSettingsResponse{EntryPath: prefix, Enabled: prefix != ""}
}

// HandleEntryPathSettings manages the secret panel entry prefix. The prefix is
// a knock path: unauthenticated visitors only reach the login page after
// visiting /<prefix>, everyone else gets a 404.
func HandleEntryPathSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: entryPathSettingsStatus()})
	case http.MethodPut:
		updateEntryPathSettings(w, r)
	default:
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
	}
}

func updateEntryPathSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		EntryPath string `json:"entry_path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid request body"})
		return
	}
	normalized, err := config.NormalizeEntryPath(req.EntryPath)
	if err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: err.Error()})
		return
	}
	previous := config.AppConfig.EntryPath
	config.AppConfig.EntryPath = normalized
	if err := config.SaveConfig(); err != nil {
		config.AppConfig.EntryPath = previous
		jsonResponse(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Save entry path failed"})
		return
	}
	detail := "(disabled - panel reachable from root)"
	if normalized != "" {
		detail = "/" + normalized
	}
	auditRequest(r, "settings.entry_path", "Panel entry", detail, true, "")
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Message: "Entry path saved", Data: entryPathSettingsStatus()})
}
