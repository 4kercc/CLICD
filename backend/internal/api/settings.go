package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"clicd/internal/config"
	"clicd/internal/telegram"

	"golang.org/x/crypto/bcrypt"
)

type LoginLog struct {
	Time      string `json:"time"`
	Username  string `json:"username"`
	IP        string `json:"ip"`
	UserAgent string `json:"user_agent"`
	Success   bool   `json:"success"`
}

var loginLogs = make([]LoginLog, 0)

// HandleLanguage returns or updates the global panel language.
func HandleLanguage(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: map[string]string{
			"language": config.NormalizeLanguage(config.AppConfig.Language),
		}})
	case http.MethodPost, http.MethodPut:
		var req struct {
			Language string `json:"language"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid request body"})
			return
		}
		config.AppConfig.Language = config.NormalizeLanguage(req.Language)
		if err := config.SaveConfig(); err != nil {
			jsonResponse(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Failed to save language"})
			return
		}
		jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: map[string]string{
			"language": config.AppConfig.Language,
		}})
	default:
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
	}
}

// HandleTaskQueueSettings returns or updates the global task concurrency limit.
func HandleTaskQueueSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: globalQueue.Settings()})
	case http.MethodPut, http.MethodPost:
		var req struct {
			Concurrency int `json:"concurrency"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid request body"})
			return
		}
		if req.Concurrency < 1 || req.Concurrency > config.MaxTaskConcurrency {
			jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "任务并发数必须在 1 到 16 之间"})
			return
		}
		previous := config.AppConfig.TaskConcurrency
		config.AppConfig.TaskConcurrency = req.Concurrency
		if err := config.SaveConfig(); err != nil {
			config.AppConfig.TaskConcurrency = previous
			jsonResponse(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "保存任务队列设置失败"})
			return
		}
		globalQueue.SetConcurrency(req.Concurrency)
		auditRequest(r, "settings.task_queue", "task_concurrency", fmt.Sprintf("concurrency=%d", req.Concurrency), true, "")
		jsonResponse(w, http.StatusOK, APIResponse{Success: true, Message: "任务队列设置已保存", Data: globalQueue.Settings()})
	default:
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
	}
}

// RecordLoginLog adds a login attempt to the log (persisted to config)
func RecordLoginLog(username, ip, userAgent string, success bool) {
	config.AddLoginLog(username, ip, userAgent, success)

	log := LoginLog{
		Time:      time.Now().UTC().Format("2006-01-02 15:04:05 UTC"),
		Username:  username,
		IP:        ip,
		UserAgent: userAgent,
		Success:   success,
	}
	loginLogs = append(loginLogs, log)
	if len(loginLogs) > 200 {
		loginLogs = loginLogs[len(loginLogs)-200:]
	}
}

// RestoreLoginLogs restores login logs from config
func RestoreLoginLogs() {
	for _, l := range config.AppConfig.LoginLogs {
		loginLogs = append(loginLogs, LoginLog{
			Time:      l.Time,
			Username:  l.Username,
			IP:        l.IP,
			UserAgent: l.UserAgent,
			Success:   l.Success,
		})
	}
}

// HandleLoginLogs returns login history
func HandleLoginLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
		return
	}
	if !requireScope(w, r, "loginlog:read") {
		return
	}

	// Return in reverse (newest first)
	reversed := make([]LoginLog, len(loginLogs))
	for i, l := range loginLogs {
		reversed[len(loginLogs)-1-i] = l
	}
	if reversed == nil {
		reversed = []LoginLog{}
	}

	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: reversed})
}

// HandleAdminPasswordChange changes admin password
func HandleAdminPasswordChange(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
		return
	}

	var req struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid request body"})
		return
	}

	if len(req.NewPassword) < 6 {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "新密码至少 6 位"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(config.AppConfig.AdminPassHash), []byte(req.OldPassword)); err != nil {
		jsonResponse(w, http.StatusUnauthorized, APIResponse{Success: false, Message: "当前密码不正确"})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		jsonResponse(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "密码加密失败"})
		return
	}

	config.AppConfig.AdminPassHash = string(hash)
	if err := config.SaveConfig(); err != nil {
		jsonResponse(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "保存配置失败"})
		return
	}

	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Message: "密码修改成功"})
}

// HandleAdminUsernameChange changes admin username
func HandleAdminUsernameChange(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
		return
	}

	var req struct {
		NewUsername string `json:"new_username"`
		Password    string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid request body"})
		return
	}

	if len(req.NewUsername) < 3 {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "用户名至少 3 位"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(config.AppConfig.AdminPassHash), []byte(req.Password)); err != nil {
		jsonResponse(w, http.StatusUnauthorized, APIResponse{Success: false, Message: "密码不正确"})
		return
	}

	config.AppConfig.AdminUser = req.NewUsername
	if err := config.SaveConfig(); err != nil {
		jsonResponse(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "保存配置失败"})
		return
	}

	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Message: "用户名修改成功"})
}

// Telegram Settings DTO
type TelegramSettingsResponse struct {
	Enabled      bool    `json:"enabled"`
	BotToken     string  `json:"bot_token"`
	AdminChatIDs []int64 `json:"admin_chat_ids"`
	NotifyAlerts bool    `json:"notify_alerts"`
	NotifyEvents bool    `json:"notify_events"`
	NotifyLogins bool    `json:"notify_logins"`
	ProxyURL     string  `json:"proxy_url"`
}

type TelegramSettingsRequest struct {
	Enabled      bool    `json:"enabled"`
	BotToken     string  `json:"bot_token"`
	AdminChatIDs []int64 `json:"admin_chat_ids"`
	NotifyAlerts bool    `json:"notify_alerts"`
	NotifyEvents bool    `json:"notify_events"`
	NotifyLogins bool    `json:"notify_logins"`
	ProxyURL     string  `json:"proxy_url"`
}

func maskToken(token string) string {
	token = strings.TrimSpace(token)
	if len(token) <= 10 {
		if len(token) == 0 {
			return ""
		}
		return "******"
	}
	return token[:6] + "..." + token[len(token)-4:]
}

// HandleTelegramSettings returns or updates Telegram Bot settings
func HandleTelegramSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if !requireScope(w, r, "admin:access") {
			return
		}
		tg := config.AppConfig.Telegram
		jsonResponse(w, http.StatusOK, APIResponse{
			Success: true,
			Data: TelegramSettingsResponse{
				Enabled:      tg.Enabled,
				BotToken:     maskToken(tg.BotToken),
				AdminChatIDs: tg.AdminChatIDs,
				NotifyAlerts: tg.NotifyAlerts,
				NotifyEvents: tg.NotifyEvents,
				NotifyLogins: tg.NotifyLogins,
				ProxyURL:     tg.ProxyURL,
			},
		})
	case http.MethodPut, http.MethodPost:
		if !requireScope(w, r, "admin:access") {
			return
		}
		var req TelegramSettingsRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid request body"})
			return
		}

		cleanIDs := make([]int64, 0, len(req.AdminChatIDs))
		seen := map[int64]bool{}
		for _, id := range req.AdminChatIDs {
			if id != 0 && !seen[id] {
				cleanIDs = append(cleanIDs, id)
				seen[id] = true
			}
		}

		newToken := strings.TrimSpace(req.BotToken)
		// 如果前端传入的是脱敏掩码，则保留原 token 不变
		if strings.Contains(newToken, "...") && len(newToken) <= 15 {
			newToken = config.AppConfig.Telegram.BotToken
		}

		config.AppConfig.Telegram = config.TelegramConfig{
			Enabled:      req.Enabled,
			BotToken:     newToken,
			AdminChatIDs: cleanIDs,
			NotifyAlerts: req.NotifyAlerts,
			NotifyEvents: req.NotifyEvents,
			NotifyLogins: req.NotifyLogins,
			ProxyURL:     strings.TrimSpace(req.ProxyURL),
		}

		if err := config.SaveConfig(); err != nil {
			jsonResponse(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Failed to save configuration"})
			return
		}

		// Restart bot with new config
		telegram.Global().Restart()

		auditRequest(r, "settings.telegram", "telegram_bot", fmt.Sprintf("enabled=%v, chats=%d", req.Enabled, len(cleanIDs)), true, "")
		jsonResponse(w, http.StatusOK, APIResponse{
			Success: true,
			Message: "Telegram Bot 设置已保存并即时生效",
			Data: TelegramSettingsResponse{
				Enabled:      config.AppConfig.Telegram.Enabled,
				BotToken:     maskToken(config.AppConfig.Telegram.BotToken),
				AdminChatIDs: config.AppConfig.Telegram.AdminChatIDs,
				NotifyAlerts: config.AppConfig.Telegram.NotifyAlerts,
				NotifyEvents: config.AppConfig.Telegram.NotifyEvents,
				NotifyLogins: config.AppConfig.Telegram.NotifyLogins,
				ProxyURL:     config.AppConfig.Telegram.ProxyURL,
			},
		})
	default:
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
	}
}

// HandleTelegramTest sends a test ping message to the configured or supplied Telegram chats
func HandleTelegramTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
		return
	}
	if !requireScope(w, r, "admin:access") {
		return
	}

	var req TelegramSettingsRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	token := strings.TrimSpace(req.BotToken)
	if token == "" || (strings.Contains(token, "...") && len(token) <= 15) {
		token = config.AppConfig.Telegram.BotToken
	}
	chatIDs := req.AdminChatIDs
	if len(chatIDs) == 0 {
		chatIDs = config.AppConfig.Telegram.AdminChatIDs
	}
	proxy := strings.TrimSpace(req.ProxyURL)
	if proxy == "" {
		proxy = config.AppConfig.Telegram.ProxyURL
	}

	if err := telegram.Global().SendTestMessage(token, chatIDs, proxy); err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "发送测试消息失败: " + err.Error()})
		return
	}

	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Message: "测试消息已成功发送至 Telegram！"})
}
