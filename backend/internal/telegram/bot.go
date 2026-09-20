package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"clicd/internal/config"
)

// BotClient wraps Telegram Bot API interactions
type BotClient struct {
	mu         sync.RWMutex
	cfg        config.TelegramConfig
	httpClient *http.Client
	cancel     context.CancelFunc
	running    bool
}

var globalBot = &BotClient{
	httpClient: &http.Client{Timeout: 30 * time.Second},
}

// Global accessor
func Global() *BotClient {
	return globalBot
}

// Start launches the Telegram Bot background long polling loop if enabled
func (b *BotClient) Start() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.cfg = config.AppConfig.Telegram
	if !b.cfg.Enabled || strings.TrimSpace(b.cfg.BotToken) == "" || len(b.cfg.AdminChatIDs) == 0 {
		return
	}

	if b.running {
		return
	}

	b.setupHTTPClient()

	ctx, cancel := context.WithCancel(context.Background())
	b.cancel = cancel
	b.running = true

	go b.pollUpdates(ctx)
	go b.registerBotCommands()
	log.Printf("[Telegram] Bot service started successfully")
}

// Stop shuts down the polling loop
func (b *BotClient) Stop() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.cancel != nil {
		b.cancel()
		b.cancel = nil
	}
	b.running = false
	log.Printf("[Telegram] Bot service stopped")
}

// Restart re-applies configuration and starts/stops as needed
func (b *BotClient) Restart() {
	b.Stop()
	b.Start()
}

func (b *BotClient) setupHTTPClient() {
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
	}
	if strings.TrimSpace(b.cfg.ProxyURL) != "" {
		if proxyURL, err := url.Parse(strings.TrimSpace(b.cfg.ProxyURL)); err == nil {
			transport.Proxy = http.ProxyURL(proxyURL)
		}
	}
	b.httpClient = &http.Client{
		Timeout:   35 * time.Second,
		Transport: transport,
	}
}

func (b *BotClient) isAuthorized(chatID int64) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, id := range b.cfg.AdminChatIDs {
		if id == chatID {
			return true
		}
	}
	return false
}

// SendTestMessage sends a test ping to all configured admin chat IDs
func (b *BotClient) SendTestMessage(token string, chatIDs []int64, proxy string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return fmt.Errorf("bot token is empty")
	}
	if len(chatIDs) == 0 {
		return fmt.Errorf("no admin chat IDs specified")
	}

	client := &http.Client{Timeout: 15 * time.Second}
	if strings.TrimSpace(proxy) != "" {
		if proxyURL, err := url.Parse(strings.TrimSpace(proxy)); err == nil {
			client.Transport = &http.Transport{Proxy: http.ProxyURL(proxyURL)}
		}
	}

	msg := fmt.Sprintf("🎉 *CLICD Telegram Bot 连接成功！*\n\n母鸡节点：`%s`\n当前时间：%s\n\n您可以通过发送 `/menu` 或 `/status` 唤起控制面板菜单。",
		getHostname(), time.Now().Format("2006-01-02 15:04:05"))

	for _, chatID := range chatIDs {
		if err := sendRawMessage(client, token, chatID, msg, "Markdown", nil); err != nil {
			return fmt.Errorf("failed to send test message to chat %d: %w", chatID, err)
		}
	}
	return nil
}

// SendSecurityAlert sends a security alert to all authorized admin chats
func (b *BotClient) SendSecurityAlert(alertType, name, severity, detail, srcIP, dstIP string, port int) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if !b.cfg.Enabled || !b.cfg.NotifyAlerts || len(b.cfg.AdminChatIDs) == 0 || b.cfg.BotToken == "" {
		return
	}

	icon := "⚠️"
	if severity == "critical" || severity == "high" {
		icon = "🚨"
	}

	msg := fmt.Sprintf("%s *【安全警报】检测到异常行为*\n\n"+
		"• *目标容器*：`%s`\n"+
		"• *警报类型*：`%s`\n"+
		"• *危险等级*：`%s`\n"+
		"• *来源地址*：`%s`\n"+
		"• *目标地址*：`%s:%d`\n"+
		"• *详情描述*：`%s`\n"+
		"• *触发时间*：%s",
		icon, name, alertType, strings.ToUpper(severity), srcIP, dstIP, port, detail, time.Now().Format("2006-01-02 15:04:05"),
	)

	keyboard := &InlineKeyboardMarkup{
		InlineKeyboard: [][]InlineKeyboardButton{
			{
				{Text: "📦 查看所有实例", CallbackData: "menu:list:0"},
				{Text: "📊 宿主机状态", CallbackData: "menu:status"},
			},
		},
	}

	for _, chatID := range b.cfg.AdminChatIDs {
		_ = sendRawMessage(b.httpClient, b.cfg.BotToken, chatID, msg, "Markdown", keyboard)
	}
}

// SendEventNotification sends general lifecycle or traffic notifications
func (b *BotClient) SendEventNotification(title, detail string) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if !b.cfg.Enabled || !b.cfg.NotifyEvents || len(b.cfg.AdminChatIDs) == 0 || b.cfg.BotToken == "" {
		return
	}

	msg := fmt.Sprintf("🔔 *【面板通知】%s*\n\n%s\n\n_时间：%s_", title, detail, time.Now().Format("2006-01-02 15:04:05"))
	for _, chatID := range b.cfg.AdminChatIDs {
		_ = sendRawMessage(b.httpClient, b.cfg.BotToken, chatID, msg, "Markdown", nil)
	}
}

// SendLoginNotification sends a notification when a user or administrator logs in successfully
func (b *BotClient) SendLoginNotification(username, role, ip, userAgent string) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if !b.cfg.Enabled || !b.cfg.NotifyLogins || len(b.cfg.AdminChatIDs) == 0 || b.cfg.BotToken == "" {
		return
	}

	hostname := getHostname()
	msg := fmt.Sprintf("🔐 *【登录提醒】控制面板登录成功*\n\n"+
		"• *登录账号*：`%s` (%s)\n"+
		"• *来源 IP*：`%s`\n"+
		"• *客户端*：`%s`\n"+
		"• *节点主机*：`%s`\n"+
		"• *登录时间*：%s",
		username, role, ip, userAgent, hostname, time.Now().Format("2006-01-02 15:04:05"))

	for _, chatID := range b.cfg.AdminChatIDs {
		if err := sendRawMessage(b.httpClient, b.cfg.BotToken, chatID, msg, "Markdown", nil); err != nil {
			log.Printf("[Telegram] Failed to push login notification for %s to chat %d: %v", username, chatID, err)
		}
	}
	log.Printf("[Telegram] Login notification pushed: user=%s role=%s ip=%s", username, role, ip)
}

// Telegram Bot API Models
type Update struct {
	UpdateID      int            `json:"update_id"`
	Message       *Message       `json:"message,omitempty"`
	CallbackQuery *CallbackQuery `json:"callback_query,omitempty"`
}

type Message struct {
	MessageID int    `json:"message_id"`
	From      *User  `json:"from,omitempty"`
	Chat      *Chat  `json:"chat"`
	Text      string `json:"text,omitempty"`
	Date      int    `json:"date"`
}

type CallbackQuery struct {
	ID      string   `json:"id"`
	From    *User    `json:"from"`
	Message *Message `json:"message,omitempty"`
	Data    string   `json:"data,omitempty"`
}

type User struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	Username  string `json:"username,omitempty"`
}

type Chat struct {
	ID    int64  `json:"id"`
	Type  string `json:"type"`
	Title string `json:"title,omitempty"`
}

type InlineKeyboardMarkup struct {
	InlineKeyboard [][]InlineKeyboardButton `json:"inline_keyboard"`
}

type InlineKeyboardButton struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data,omitempty"`
	URL          string `json:"url,omitempty"`
}

// Long polling loop
func (b *BotClient) pollUpdates(ctx context.Context) {
	offset := 0
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		b.mu.RLock()
		token := b.cfg.BotToken
		client := b.httpClient
		b.mu.RUnlock()

		if token == "" {
			time.Sleep(5 * time.Second)
			continue
		}

		updates, err := getUpdates(client, token, offset, 20)
		if err != nil {
			time.Sleep(3 * time.Second)
			continue
		}

		for _, update := range updates {
			if update.UpdateID >= offset {
				offset = update.UpdateID + 1
			}
			go b.handleUpdate(update)
		}
	}
}

func (b *BotClient) handleUpdate(u Update) {
	if u.Message != nil {
		chatID := u.Message.Chat.ID
		if !b.isAuthorized(chatID) {
			// Ignore unauthorized users
			return
		}
		b.handleCommand(u.Message)
		return
	}

	if u.CallbackQuery != nil {
		chatID := int64(0)
		if u.CallbackQuery.Message != nil {
			chatID = u.CallbackQuery.Message.Chat.ID
		} else if u.CallbackQuery.From != nil {
			chatID = u.CallbackQuery.From.ID
		}
		if !b.isAuthorized(chatID) {
			return
		}
		b.handleCallback(u.CallbackQuery)
		return
	}
}

func (b *BotClient) handleCommand(msg *Message) {
	text := strings.TrimSpace(msg.Text)
	if text == "" {
		return
	}
	parts := strings.Fields(text)
	cmd := strings.ToLower(parts[0])
	// Strip @botname suffix if present
	if idx := strings.Index(cmd, "@"); idx != -1 {
		cmd = cmd[:idx]
	}

	switch cmd {
	case "/start", "/menu", "/help":
		b.sendMainMenu(msg.Chat.ID, msg.MessageID, false)
	case "/status":
		b.sendStatus(msg.Chat.ID, msg.MessageID, false)
	case "/list":
		b.sendContainerList(msg.Chat.ID, msg.MessageID, 0, false)
	case "/batch":
		b.sendBatchMenu(msg.Chat.ID, msg.MessageID, false)
	case "/web":
		b.sendWebAccessMenu(msg.Chat.ID, msg.MessageID, false)
	default:
		// Default to main menu
		b.sendMainMenu(msg.Chat.ID, msg.MessageID, false)
	}
}

func (b *BotClient) handleCallback(cb *CallbackQuery) {
	data := cb.Data
	chatID := cb.Message.Chat.ID
	msgID := cb.Message.MessageID

	// Answer callback to dismiss loading wheel
	_ = answerCallback(b.httpClient, b.cfg.BotToken, cb.ID, "")

	if data == "menu:main" {
		b.sendMainMenu(chatID, msgID, true)
		return
	}
	if data == "menu:status" {
		b.sendStatus(chatID, msgID, true)
		return
	}
	if strings.HasPrefix(data, "menu:list:") {
		page, _ := strconv.Atoi(strings.TrimPrefix(data, "menu:list:"))
		b.sendContainerList(chatID, msgID, page, true)
		return
	}
	if data == "menu:batch" {
		b.sendBatchMenu(chatID, msgID, true)
		return
	}
	if data == "menu:web" {
		b.sendWebAccessMenu(chatID, msgID, true)
		return
	}
	if strings.HasPrefix(data, "web:toggle:") {
		action := strings.TrimPrefix(data, "web:toggle:")
		b.executeWebAccessToggle(chatID, msgID, action)
		return
	}

	// Container detail menu: c:detail:<id>
	if strings.HasPrefix(data, "c:detail:") {
		id, _ := strconv.Atoi(strings.TrimPrefix(data, "c:detail:"))
		b.sendContainerDetail(chatID, msgID, id, true)
		return
	}

	// Actions: c:action:<action>:<id>
	if strings.HasPrefix(data, "c:act:") {
		parts := strings.Split(data, ":")
		if len(parts) >= 4 {
			action := parts[2]
			id, _ := strconv.Atoi(parts[3])
			b.handleContainerAction(chatID, msgID, id, action)
		}
		return
	}

	// Confirm actions: c:confirm:<action>:<id>
	if strings.HasPrefix(data, "c:confirm:") {
		parts := strings.Split(data, ":")
		if len(parts) >= 4 {
			action := parts[2]
			id, _ := strconv.Atoi(parts[3])
			b.executeContainerAction(chatID, msgID, id, action)
		}
		return
	}

	// Batch sub menus
	if data == "batch:menu:config" {
		b.sendBatchConfigMenu(chatID, msgID, true)
		return
	}
	if data == "batch:menu:create" {
		b.sendBatchCreateMenu(chatID, msgID, true)
		return
	}
	if data == "batch:ask:restore_snap" {
		b.sendBatchRestoreSnapConfirm(chatID, msgID, true)
		return
	}

	// Batch actions: batch:confirm:<action>
	if strings.HasPrefix(data, "batch:confirm:") {
		action := strings.TrimPrefix(data, "batch:confirm:")
		b.executeBatchAction(chatID, msgID, action)
		return
	}

	// Batch config apply: batch:setcfg:<profile>
	if strings.HasPrefix(data, "batch:setcfg:") {
		profile := strings.TrimPrefix(data, "batch:setcfg:")
		b.executeBatchConfig(chatID, msgID, profile)
		return
	}

	// Batch create quick apply: batch:do_create:<type>
	if strings.HasPrefix(data, "batch:do_create:") {
		preset := strings.TrimPrefix(data, "batch:do_create:")
		b.executeBatchCreate(chatID, msgID, preset)
		return
	}
}

func (b *BotClient) sendMainMenu(chatID int64, msgID int, edit bool) {
	webStatusDesc := "🟢 正常开放"
	if config.AppConfig.WebAccessDisabled {
		webStatusDesc = "🔒 已关闭封锁"
	}

	text := fmt.Sprintf("🎮 *CLICD 虚拟化管理控制台*\n\n"+
		"• *宿主机*：`%s`\n"+
		"• *Web 入口*：%s\n"+
		"• *面板版本*：`v1.20.5`\n"+
		"• *系统时间*：%s\n\n"+
		"请选择您要执行的操作：",
		getHostname(), webStatusDesc, time.Now().Format("2006-01-02 15:04:05"))

	keyboard := &InlineKeyboardMarkup{
		InlineKeyboard: [][]InlineKeyboardButton{
			{
				{Text: "📊 宿主机监控状态", CallbackData: "menu:status"},
				{Text: "📦 实例管理列表", CallbackData: "menu:list:0"},
			},
			{
				{Text: "⚡ 批量控制中心", CallbackData: "menu:batch"},
				{Text: "🛡️ Web 访问安全开关", CallbackData: "menu:web"},
			},
			{
				{Text: "🔄 刷新主菜单", CallbackData: "menu:main"},
			},
		},
	}

	if edit && msgID > 0 {
		_ = editRawMessage(b.httpClient, b.cfg.BotToken, chatID, msgID, text, "Markdown", keyboard)
	} else {
		_ = sendRawMessage(b.httpClient, b.cfg.BotToken, chatID, text, "Markdown", keyboard)
	}
}

func (b *BotClient) sendStatus(chatID int64, msgID int, edit bool) {
	// Query host metrics
	containers := config.AppConfig.Containers
	runningCount := 0
	for _, c := range containers {
		if c.Status == "running" {
			runningCount++
		}
	}

	text := fmt.Sprintf("📊 *宿主机实时状态监控*\n\n"+
		"• *主机名称*：`%s`\n"+
		"• *实例统计*：共 `%d` 台 · 🟢 `%d` 运行中 · 🔴 `%d` 停止\n"+
		"• *系统负载*：%s\n"+
		"• *内存使用*：%s\n"+
		"• *磁盘空间*：%s\n"+
		"• *网络端口*：NAT4 已用 `%d` 端口\n\n"+
		"_更新于 %s_",
		getHostname(), len(containers), runningCount, len(containers)-runningCount,
		getHostLoadSummary(), getHostMemorySummary(), getHostDiskSummary(),
		len(config.AppConfig.Containers),
		time.Now().Format("15:04:05"),
	)

	keyboard := &InlineKeyboardMarkup{
		InlineKeyboard: [][]InlineKeyboardButton{
			{
				{Text: "🔄 刷新状态", CallbackData: "menu:status"},
				{Text: "📦 实例列表", CallbackData: "menu:list:0"},
			},
			{
				{Text: "🔙 返回主菜单", CallbackData: "menu:main"},
			},
		},
	}

	if edit && msgID > 0 {
		_ = editRawMessage(b.httpClient, b.cfg.BotToken, chatID, msgID, text, "Markdown", keyboard)
	} else {
		_ = sendRawMessage(b.httpClient, b.cfg.BotToken, chatID, text, "Markdown", keyboard)
	}
}

func (b *BotClient) sendContainerList(chatID int64, msgID int, page int, edit bool) {
	containers := append([]config.Container(nil), config.AppConfig.Containers...)
	sort.Slice(containers, func(i, j int) bool { return containers[i].ID < containers[j].ID })

	pageSize := 6
	if page < 0 {
		page = 0
	}
	totalPages := (len(containers) + pageSize - 1) / pageSize
	if totalPages == 0 {
		totalPages = 1
	}
	if page >= totalPages {
		page = totalPages - 1
	}

	start := page * pageSize
	end := start + pageSize
	if end > len(containers) {
		end = len(containers)
	}

	var rows [][]InlineKeyboardButton
	for i := start; i < end; i++ {
		c := containers[i]
		statusIcon := "🔴"
		if c.Status == "running" {
			statusIcon = "🟢"
		}
		btnText := fmt.Sprintf("%s %s (ID:%d) · %s", statusIcon, c.Name, c.ID, strings.ToUpper(c.Virtualization))
		rows = append(rows, []InlineKeyboardButton{
			{Text: btnText, CallbackData: fmt.Sprintf("c:detail:%d", c.ID)},
		})
	}

	var navRow []InlineKeyboardButton
	if page > 0 {
		navRow = append(navRow, InlineKeyboardButton{Text: "⬅️ 上一页", CallbackData: fmt.Sprintf("menu:list:%d", page-1)})
	}
	navRow = append(navRow, InlineKeyboardButton{Text: fmt.Sprintf("📄 %d/%d", page+1, totalPages), CallbackData: fmt.Sprintf("menu:list:%d", page)})
	if page < totalPages-1 {
		navRow = append(navRow, InlineKeyboardButton{Text: "下一页 ➡️", CallbackData: fmt.Sprintf("menu:list:%d", page+1)})
	}
	rows = append(rows, navRow)

	rows = append(rows, []InlineKeyboardButton{
		{Text: "🔄 刷新列表", CallbackData: fmt.Sprintf("menu:list:%d", page)},
		{Text: "🔙 主菜单", CallbackData: "menu:main"},
	})

	text := fmt.Sprintf("📦 *实例管理列表 (第 %d/%d 页)*\n\n点击任意实例进入操作面板进行开关机、快照或密码重置：", page+1, totalPages)

	keyboard := &InlineKeyboardMarkup{InlineKeyboard: rows}
	if edit && msgID > 0 {
		_ = editRawMessage(b.httpClient, b.cfg.BotToken, chatID, msgID, text, "Markdown", keyboard)
	} else {
		_ = sendRawMessage(b.httpClient, b.cfg.BotToken, chatID, text, "Markdown", keyboard)
	}
}

func (b *BotClient) sendContainerDetail(chatID int64, msgID int, id int, edit bool) {
	c := config.FindContainer(id)
	if c == nil {
		_ = editRawMessage(b.httpClient, b.cfg.BotToken, chatID, msgID, "❌ *未找到该实例*", "Markdown", &InlineKeyboardMarkup{
			InlineKeyboard: [][]InlineKeyboardButton{{{Text: "🔙 返回列表", CallbackData: "menu:list:0"}}},
		})
		return
	}

	statusIcon := "🔴 已停止"
	if c.Status == "running" {
		statusIcon = "🟢 运行中"
	}

	ipStr := c.IP
	if ipStr == "" {
		ipStr = "未分配"
	}
	macStr := c.MACAddress
	if macStr == "" {
		macStr = "未指定"
	}

	// 密码信息（使用 Telegram剧透/剧透模糊语法 ||password||，点击可查看与复制）
	pwdStr := c.SSHPassword
	if strings.TrimSpace(pwdStr) == "" {
		pwdStr = "_未设置/密钥登录_"
	} else {
		pwdStr = fmt.Sprintf("||`%s`||", pwdStr)
	}

	// 解析宿主机公网 IPv4
	hostPublicIP := getHostPrimaryPublicIPv4()

	portMapSummary := ""
	for i, pm := range c.PortMappings {
		if i < 6 {
			displayIP := strings.TrimSpace(pm.HostIP)
			if displayIP == "" {
				displayIP = hostPublicIP
			}
			portMapSummary += fmt.Sprintf("\n  • `%s:%d -> %d (%s)`", displayIP, pm.HostPort, pm.ContainerPort, strings.ToUpper(pm.Protocol))
		}
	}
	if len(c.PortMappings) > 6 {
		portMapSummary += fmt.Sprintf("\n  • _...共 %d 个端口映射_", len(c.PortMappings))
	}
	if portMapSummary == "" {
		portMapSummary = " 无"
	}

	text := fmt.Sprintf("🖥️ *实例详情：【%s】*\n\n"+
		"• *实例 ID*：`%d`\n"+
		"• *运行时名*：`%s` (%s)\n"+
		"• *当前状态*：%s\n"+
		"• *内网 IP*：`%s`\n"+
		"• *硬件 MAC*：`%s`\n"+
		"• *计算配置*：`%.0f 核 / %d MB / %d GB`\n"+
		"• *密码信息*：%s\n"+
		"• *网络端口*：%s\n\n"+
		"_请选择要执行的操作：_",
		c.Name, c.ID, c.LxcName(), strings.ToUpper(c.Virtualization), statusIcon,
		ipStr, macStr, c.VCPU, c.RAMMB, c.DiskGB, pwdStr, portMapSummary,
	)

	var powerBtn InlineKeyboardButton
	if c.Status == "running" {
		powerBtn = InlineKeyboardButton{Text: "🔴 关机", CallbackData: fmt.Sprintf("c:act:stop:%d", c.ID)}
	} else {
		powerBtn = InlineKeyboardButton{Text: "🟢 开机", CallbackData: fmt.Sprintf("c:act:start:%d", c.ID)}
	}

	keyboard := &InlineKeyboardMarkup{
		InlineKeyboard: [][]InlineKeyboardButton{
			{
				powerBtn,
				{Text: "🔄 重启", CallbackData: fmt.Sprintf("c:act:restart:%d", c.ID)},
			},
			{
				{Text: "📸 创建快照", CallbackData: fmt.Sprintf("c:act:snap:%d", c.ID)},
				{Text: "🔑 重置密码", CallbackData: fmt.Sprintf("c:act:pwd:%d", c.ID)},
			},
			{
				{Text: "🔄 刷新详情", CallbackData: fmt.Sprintf("c:detail:%d", c.ID)},
				{Text: "🔙 返回列表", CallbackData: "menu:list:0"},
			},
		},
	}

	if edit && msgID > 0 {
		_ = editRawMessage(b.httpClient, b.cfg.BotToken, chatID, msgID, text, "Markdown", keyboard)
	} else {
		_ = sendRawMessage(b.httpClient, b.cfg.BotToken, chatID, text, "Markdown", keyboard)
	}
}

func (b *BotClient) handleContainerAction(chatID int64, msgID int, id int, action string) {
	c := config.FindContainer(id)
	if c == nil {
		return
	}

	switch action {
	case "start":
		b.executeContainerAction(chatID, msgID, id, "start")
	case "stop":
		text := fmt.Sprintf("⚠️ *危险确认：确定要对【%s (ID:%d)】执行关机操作吗？*", c.Name, c.ID)
		keyboard := &InlineKeyboardMarkup{
			InlineKeyboard: [][]InlineKeyboardButton{
				{
					{Text: "✅ 确认关机", CallbackData: fmt.Sprintf("c:confirm:stop:%d", c.ID)},
					{Text: "❌ 取消", CallbackData: fmt.Sprintf("c:detail:%d", c.ID)},
				},
			},
		}
		_ = editRawMessage(b.httpClient, b.cfg.BotToken, chatID, msgID, text, "Markdown", keyboard)
	case "restart":
		text := fmt.Sprintf("⚠️ *确定要重启【%s (ID:%d)】吗？*", c.Name, c.ID)
		keyboard := &InlineKeyboardMarkup{
			InlineKeyboard: [][]InlineKeyboardButton{
				{
					{Text: "✅ 确认重启", CallbackData: fmt.Sprintf("c:confirm:restart:%d", c.ID)},
					{Text: "❌ 取消", CallbackData: fmt.Sprintf("c:detail:%d", c.ID)},
				},
			},
		}
		_ = editRawMessage(b.httpClient, b.cfg.BotToken, chatID, msgID, text, "Markdown", keyboard)
	case "snap":
		b.executeContainerAction(chatID, msgID, id, "snap")
	case "pwd":
		text := fmt.Sprintf("⚠️ *确定要为【%s (ID:%d)】重置为新的随机强密码吗？*", c.Name, c.ID)
		keyboard := &InlineKeyboardMarkup{
			InlineKeyboard: [][]InlineKeyboardButton{
				{
					{Text: "✅ 确认重置密码", CallbackData: fmt.Sprintf("c:confirm:pwd:%d", c.ID)},
					{Text: "❌ 取消", CallbackData: fmt.Sprintf("c:detail:%d", c.ID)},
				},
			},
		}
		_ = editRawMessage(b.httpClient, b.cfg.BotToken, chatID, msgID, text, "Markdown", keyboard)
	}
}

func (b *BotClient) executeContainerAction(chatID int64, msgID int, id int, action string) {
	c := config.FindContainer(id)
	if c == nil {
		return
	}

	_ = editRawMessage(b.httpClient, b.cfg.BotToken, chatID, msgID, fmt.Sprintf("⏳ 正在对【%s】执行 `%s` 操作，请稍候...", c.Name, action), "Markdown", nil)

	go func() {
		var err error
		var resultText string

		switch action {
		case "start":
			err = config.StartContainerCallback(id)
			resultText = fmt.Sprintf("🟢 *【%s】开机指令已执行完毕*", c.Name)
		case "stop":
			err = config.StopContainerCallback(id)
			resultText = fmt.Sprintf("🔴 *【%s】关机指令已执行完毕*", c.Name)
		case "restart":
			err = config.RestartContainerCallback(id)
			resultText = fmt.Sprintf("🔄 *【%s】重启指令已执行完毕*", c.Name)
		case "snap":
			var snapName string
			snapName, err = config.CreateSnapshotCallback(id, "telegram:admin")
			resultText = fmt.Sprintf("📸 *【%s】快照创建成功*\n快照标识：`%s`", c.Name, snapName)
		case "pwd":
			var newPwd string
			newPwd, err = config.ResetPasswordCallback(id)
			resultText = fmt.Sprintf("🔑 *【%s】密码重置成功*\n\n新密码：`%s`\n\n_请妥善保存此密码_", c.Name, newPwd)
		}

		if err != nil {
			resultText = fmt.Sprintf("❌ *【%s】执行 %s 失败*\n\n错误信息：`%v`", c.Name, action, err)
		}

		keyboard := &InlineKeyboardMarkup{
			InlineKeyboard: [][]InlineKeyboardButton{
				{{Text: "🔍 查看实例详情", CallbackData: fmt.Sprintf("c:detail:%d", id)}},
				{{Text: "📦 返回实例列表", CallbackData: "menu:list:0"}},
			},
		}

		_ = editRawMessage(b.httpClient, b.cfg.BotToken, chatID, msgID, resultText, "Markdown", keyboard)
	}()
}

func (b *BotClient) sendBatchMenu(chatID int64, msgID int, edit bool) {
	containers := config.AppConfig.Containers
	text := fmt.Sprintf("⚡ *批量控制中心*\n\n当前共有 `%d` 个容器/虚拟机。\n请选择要执行的批量任务（均受后台并发队列安全节流）：", len(containers))

	keyboard := &InlineKeyboardMarkup{
		InlineKeyboard: [][]InlineKeyboardButton{
			{
				{Text: "🟢 批量开机", CallbackData: "batch:confirm:start"},
				{Text: "🔴 批量关机", CallbackData: "batch:confirm:stop"},
				{Text: "🔄 批量重启", CallbackData: "batch:confirm:restart"},
			},
			{
				{Text: "📷 批量创建快照", CallbackData: "batch:confirm:snapshot"},
				{Text: "⏪ 批量恢复快照", CallbackData: "batch:ask:restore_snap"},
			},
			{
				{Text: "⚙️ 批量调整配置", CallbackData: "batch:menu:config"},
				{Text: "🚀 批量快速建机", CallbackData: "batch:menu:create"},
			},
			{
				{Text: "🔙 返回主菜单", CallbackData: "menu:main"},
			},
		},
	}

	if edit && msgID > 0 {
		_ = editRawMessage(b.httpClient, b.cfg.BotToken, chatID, msgID, text, "Markdown", keyboard)
	} else {
		_ = sendRawMessage(b.httpClient, b.cfg.BotToken, chatID, text, "Markdown", keyboard)
	}
}

func (b *BotClient) sendBatchConfigMenu(chatID int64, msgID int, edit bool) {
	text := "⚙️ *批量调整配置*\n\n请选择要批量应用的硬件与限速规格模板（将全量应用于现有所有实例并热生效）："
	keyboard := &InlineKeyboardMarkup{
		InlineKeyboard: [][]InlineKeyboardButton{
			{
				{Text: "🚀 性能型 (2核/2G/100M)", CallbackData: "batch:setcfg:perf"},
			},
			{
				{Text: "⚡ 标准型 (1核/1G/50M)", CallbackData: "batch:setcfg:std"},
			},
			{
				{Text: "🍃 轻量型 (1核/512M/30M)", CallbackData: "batch:setcfg:light"},
			},
			{
				{Text: "🌐 仅统一限速 (上下行100M)", CallbackData: "batch:setcfg:net100"},
			},
			{
				{Text: "🔙 返回批量中心", CallbackData: "menu:batch"},
			},
		},
	}

	if edit && msgID > 0 {
		_ = editRawMessage(b.httpClient, b.cfg.BotToken, chatID, msgID, text, "Markdown", keyboard)
	} else {
		_ = sendRawMessage(b.httpClient, b.cfg.BotToken, chatID, text, "Markdown", keyboard)
	}
}

func (b *BotClient) sendBatchCreateMenu(chatID int64, msgID int, edit bool) {
	text := "🚀 *批量快速建机*\n\n请选择批量创建模板（自动分配 NAT 端口与排队并发创建）："
	keyboard := &InlineKeyboardMarkup{
		InlineKeyboard: [][]InlineKeyboardButton{
			{
				{Text: "📦 批量开 3 台 Debian LXC (1核/1G/10G)", CallbackData: "batch:do_create:lxc3"},
			},
			{
				{Text: "📦 批量开 5 台 Debian LXC (1核/1G/10G)", CallbackData: "batch:do_create:lxc5"},
			},
			{
				{Text: "🖥️ 批量开 2 台 Debian KVM (1核/1G/10G)", CallbackData: "batch:do_create:kvm2"},
			},
			{
				{Text: "🖥️ 快速开 1 台 Debian KVM (2核/2G/20G)", CallbackData: "batch:do_create:kvm1_pro"},
			},
			{
				{Text: "🔙 返回批量中心", CallbackData: "menu:batch"},
			},
		},
	}

	if edit && msgID > 0 {
		_ = editRawMessage(b.httpClient, b.cfg.BotToken, chatID, msgID, text, "Markdown", keyboard)
	} else {
		_ = sendRawMessage(b.httpClient, b.cfg.BotToken, chatID, text, "Markdown", keyboard)
	}
}

func (b *BotClient) sendBatchRestoreSnapConfirm(chatID int64, msgID int, edit bool) {
	text := "⚠️ *危险二次确认：批量恢复最新快照*\n\n此操作将遍历所有存在快照的实例，并将其回滚至各自【最新的可用快照】。\n未建快照的实例将不受影响。\n\n确认立即执行批量回滚吗？"
	keyboard := &InlineKeyboardMarkup{
		InlineKeyboard: [][]InlineKeyboardButton{
			{
				{Text: "✅ 确认全部回滚快照", CallbackData: "batch:confirm:restore_snap"},
				{Text: "❌ 取消", CallbackData: "menu:batch"},
			},
		},
	}

	if edit && msgID > 0 {
		_ = editRawMessage(b.httpClient, b.cfg.BotToken, chatID, msgID, text, "Markdown", keyboard)
	} else {
		_ = sendRawMessage(b.httpClient, b.cfg.BotToken, chatID, text, "Markdown", keyboard)
	}
}

func (b *BotClient) executeBatchAction(chatID int64, msgID int, action string) {
	containers := config.AppConfig.Containers
	var ids []int
	for _, c := range containers {
		ids = append(ids, c.ID)
	}

	actionZh := "开机"
	switch action {
	case "stop":
		actionZh = "关机"
	case "restart":
		actionZh = "重启"
	case "snapshot":
		actionZh = "创建快照"
	case "restore_snap":
		actionZh = "恢复最新快照"
	}

	if action == "restore_snap" {
		_ = editRawMessage(b.httpClient, b.cfg.BotToken, chatID, msgID,
			"⏳ 正在执行批量快照回滚，请稍候...", "Markdown", nil)
		go func() {
			count, err := config.BatchRestoreLatestSnapshotCallback(ids)
			text := fmt.Sprintf("✅ *批量恢复快照完成*\n成功将 `%d` 台实例回滚至最新快照状态。", count)
			if err != nil {
				text = fmt.Sprintf("❌ *批量恢复快照失败*：%v", err)
			}
			keyboard := &InlineKeyboardMarkup{
				InlineKeyboard: [][]InlineKeyboardButton{
					{{Text: "⚡ 返回批量中心", CallbackData: "menu:batch"}},
					{{Text: "🔙 返回主菜单", CallbackData: "menu:main"}},
				},
			}
			_ = editRawMessage(b.httpClient, b.cfg.BotToken, chatID, msgID, text, "Markdown", keyboard)
		}()
		return
	}

	_ = editRawMessage(b.httpClient, b.cfg.BotToken, chatID, msgID,
		fmt.Sprintf("⏳ 正在向任务队列推送 `%d` 台实例的批量%s任务...", len(ids), actionZh), "Markdown", nil)

	go func() {
		err := config.BatchActionCallback(ids, action)
		text := fmt.Sprintf("✅ *批量%s任务已提交至后台队列*\n共 `%d` 台实例，面板正在按并发上限排队执行。", actionZh, len(ids))
		if err != nil {
			text = fmt.Sprintf("❌ *批量%s提交失败*：%v", actionZh, err)
		}
		keyboard := &InlineKeyboardMarkup{
			InlineKeyboard: [][]InlineKeyboardButton{
				{{Text: "📊 查看宿主机状态", CallbackData: "menu:status"}},
				{{Text: "⚡ 返回批量中心", CallbackData: "menu:batch"}},
				{{Text: "🔙 返回主菜单", CallbackData: "menu:main"}},
			},
		}
		_ = editRawMessage(b.httpClient, b.cfg.BotToken, chatID, msgID, text, "Markdown", keyboard)
	}()
}

func (b *BotClient) executeBatchConfig(chatID int64, msgID int, profile string) {
	containers := config.AppConfig.Containers
	var ids []int
	for _, c := range containers {
		ids = append(ids, c.ID)
	}

	vcpu := 0.0
	ramMB := 0
	diskGB := 0
	downMbps := -1
	upMbps := -1
	profileName := ""

	switch profile {
	case "perf":
		vcpu = 2
		ramMB = 2048
		downMbps = 100
		upMbps = 100
		profileName = "🚀 性能型 (2核/2048MB/100M)"
	case "std":
		vcpu = 1
		ramMB = 1024
		downMbps = 50
		upMbps = 50
		profileName = "⚡ 标准型 (1核/1024MB/50M)"
	case "light":
		vcpu = 1
		ramMB = 512
		downMbps = 30
		upMbps = 30
		profileName = "🍃 轻量型 (1核/512MB/30M)"
	case "net100":
		downMbps = 100
		upMbps = 100
		profileName = "🌐 统一限速 (上下行100M)"
	}

	_ = editRawMessage(b.httpClient, b.cfg.BotToken, chatID, msgID,
		fmt.Sprintf("⏳ 正在批量应用【%s】配置...", profileName), "Markdown", nil)

	go func() {
		count, err := config.BatchAdjustQuickConfigCallback(ids, vcpu, ramMB, diskGB, downMbps, upMbps)
		text := fmt.Sprintf("✅ *批量配置应用成功*\n\n• 模板规格：%s\n• 影响实例：`%d` 台\n• 运行中实例已动态完成限额热应用。", profileName, count)
		if err != nil {
			text = fmt.Sprintf("❌ *批量调整配置失败*：%v", err)
		}
		keyboard := &InlineKeyboardMarkup{
			InlineKeyboard: [][]InlineKeyboardButton{
				{{Text: "⚡ 返回批量中心", CallbackData: "menu:batch"}},
				{{Text: "🔙 返回主菜单", CallbackData: "menu:main"}},
			},
		}
		_ = editRawMessage(b.httpClient, b.cfg.BotToken, chatID, msgID, text, "Markdown", keyboard)
	}()
}

func (b *BotClient) executeBatchCreate(chatID int64, msgID int, preset string) {
	prefix := "tg-node"
	count := 1
	templateID := ""
	vcpu := 1.0
	ramMB := 1024
	diskGB := 10
	isKVM := false
	desc := ""

	switch preset {
	case "lxc3":
		prefix = "lxc-batch"
		count = 3
		templateID = "debian-bookworm"
		vcpu = 1
		ramMB = 1024
		diskGB = 10
		isKVM = false
		desc = "3 台 Debian LXC 容器 (1核/1G/10G)"
	case "lxc5":
		prefix = "lxc-batch"
		count = 5
		templateID = "debian-bookworm"
		vcpu = 1
		ramMB = 1024
		diskGB = 10
		isKVM = false
		desc = "5 台 Debian LXC 容器 (1核/1G/10G)"
	case "kvm2":
		prefix = "kvm-batch"
		count = 2
		templateID = "debian-12-generic-amd64"
		vcpu = 1
		ramMB = 1024
		diskGB = 10
		isKVM = true
		desc = "2 台 Debian KVM 虚拟机 (1核/1G/10G)"
	case "kvm1_pro":
		prefix = "kvm-pro"
		count = 1
		templateID = "debian-12-generic-amd64"
		vcpu = 2
		ramMB = 2048
		diskGB = 20
		isKVM = true
		desc = "1 台 Debian KVM 虚拟机 (2核/2G/20G)"
	}

	_ = editRawMessage(b.httpClient, b.cfg.BotToken, chatID, msgID,
		fmt.Sprintf("⏳ 正在为【%s】规划端口并推送建机任务...", desc), "Markdown", nil)

	go func() {
		enqueued, err := config.BatchCreateQuickVMCallback(prefix, count, templateID, vcpu, ramMB, diskGB, isKVM)
		text := fmt.Sprintf("✅ *批量快速建机任务已入队*\n\n• 目标规格：%s\n• 成功入队：`%d` 台\n• 面板已完成 NAT 端口自动分配，正在后台排队拉取镜像与部署启动。", desc, enqueued)
		if err != nil {
			text = fmt.Sprintf("❌ *批量建机入队失败*：%v", err)
		}
		keyboard := &InlineKeyboardMarkup{
			InlineKeyboard: [][]InlineKeyboardButton{
				{{Text: "📦 查看实例列表", CallbackData: "menu:list:0"}},
				{{Text: "⚡ 返回批量中心", CallbackData: "menu:batch"}},
				{{Text: "🔙 返回主菜单", CallbackData: "menu:main"}},
			},
		}
		_ = editRawMessage(b.httpClient, b.cfg.BotToken, chatID, msgID, text, "Markdown", keyboard)
	}()
}

func (b *BotClient) sendWebAccessMenu(chatID int64, msgID int, edit bool) {
	statusText := "🟢 当前状态：*正常开放*（允许浏览器访问面板与 API）"
	toggleBtnText := "🔒 立即关闭 Web 访问入口"
	toggleAction := "disable"
	if config.AppConfig.WebAccessDisabled {
		statusText = "🔴 当前状态：*安全封锁中*（所有浏览器与 API 均直接返回 404）"
		toggleBtnText = "🟢 立即开启 Web 访问入口"
		toggleAction = "enable"
	}

	text := fmt.Sprintf("🛡️ *Web 访问入口安全控制*\n\n"+
		"%s\n\n"+
		"💡 *安全防护说明*：\n"+
		"• 关闭 Web 入口后，底层虚拟化容器与 Telegram 机器人管理依然*完全正常运行*；\n"+
		"• 所有外部针对 Web 面板端口（`%d`）的探测、爆破、爬虫均直接返回 404 伪装阻断；\n"+
		"• 若 Telegram 发生故障，您可随时在宿主机终端输入 `clicd web on` 或 `clicd` 恢复访问。\n\n"+
		"请选择您的操作：",
		statusText, config.AppConfig.Port)

	keyboard := &InlineKeyboardMarkup{
		InlineKeyboard: [][]InlineKeyboardButton{
			{
				{Text: toggleBtnText, CallbackData: "web:toggle:" + toggleAction},
			},
			{
				{Text: "🔄 刷新状态", CallbackData: "menu:web"},
				{Text: "🔙 返回主菜单", CallbackData: "menu:main"},
			},
		},
	}

	if edit && msgID > 0 {
		_ = editRawMessage(b.httpClient, b.cfg.BotToken, chatID, msgID, text, "Markdown", keyboard)
	} else {
		_ = sendRawMessage(b.httpClient, b.cfg.BotToken, chatID, text, "Markdown", keyboard)
	}
}

func (b *BotClient) executeWebAccessToggle(chatID int64, msgID int, action string) {
	if action == "disable" {
		config.AppConfig.WebAccessDisabled = true
	} else {
		config.AppConfig.WebAccessDisabled = false
	}

	if err := config.SaveConfig(); err != nil {
		text := fmt.Sprintf("❌ *操作失败*：保存配置失败: %v", err)
		_ = editRawMessage(b.httpClient, b.cfg.BotToken, chatID, msgID, text, "Markdown", nil)
		return
	}

	// Re-render the menu with updated state
	b.sendWebAccessMenu(chatID, msgID, true)
}

func (b *BotClient) registerBotCommands() {
	b.mu.RLock()
	token := b.cfg.BotToken
	client := b.httpClient
	b.mu.RUnlock()

	if token == "" || client == nil {
		return
	}

	commands := []map[string]string{
		{"command": "menu", "description": "🎮 呼出控制台主菜单"},
		{"command": "status", "description": "📊 查看宿主机实时状态监控"},
		{"command": "list", "description": "📦 查看实例列表与单机操作"},
		{"command": "batch", "description": "⚡ 批量控制中心 (快照/配置/建机)"},
		{"command": "web", "description": "🛡️ 开启/关闭 Web 访问入口"},
		{"command": "help", "description": "ℹ️ 查看使用帮助"},
	}

	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/setMyCommands", token)
	payload := map[string]interface{}{
		"commands": commands,
	}
	data, _ := json.Marshal(payload)
	resp, err := client.Post(apiURL, "application/json", bytes.NewReader(data))
	if err != nil {
		log.Printf("[Telegram] Failed to set bot commands: %v", err)
		return
	}
	defer resp.Body.Close()
	log.Printf("[Telegram] Bot menu commands registered successfully")
}

// Low-level Telegram API helper functions
func getUpdates(client *http.Client, token string, offset, timeout int) ([]Update, error) {
	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/getUpdates?offset=%d&timeout=%d", token, offset, timeout)
	resp, err := client.Get(apiURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		OK     bool     `json:"ok"`
		Result []Update `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	if !result.OK {
		return nil, fmt.Errorf("telegram getUpdates failed")
	}
	return result.Result, nil
}

func sendRawMessage(client *http.Client, token string, chatID int64, text string, parseMode string, keyboard *InlineKeyboardMarkup) error {
	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", token)
	payload := map[string]interface{}{
		"chat_id":    chatID,
		"text":       text,
		"parse_mode": parseMode,
	}
	if keyboard != nil {
		payload["reply_markup"] = keyboard
	}

	data, _ := json.Marshal(payload)
	resp, err := client.Post(apiURL, "application/json", bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	return nil
}

func editRawMessage(client *http.Client, token string, chatID int64, msgID int, text string, parseMode string, keyboard *InlineKeyboardMarkup) error {
	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/editMessageText", token)
	payload := map[string]interface{}{
		"chat_id":    chatID,
		"message_id": msgID,
		"text":       text,
		"parse_mode": parseMode,
	}
	if keyboard != nil {
		payload["reply_markup"] = keyboard
	}

	data, _ := json.Marshal(payload)
	resp, err := client.Post(apiURL, "application/json", bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	return nil
}

func answerCallback(client *http.Client, token string, callbackID, text string) error {
	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/answerCallbackQuery", token)
	payload := map[string]interface{}{
		"callback_query_id": callbackID,
	}
	if text != "" {
		payload["text"] = text
	}
	data, _ := json.Marshal(payload)
	resp, err := client.Post(apiURL, "application/json", bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	return nil
}

func getHostPrimaryPublicIPv4() string {
	out, err := exec.Command("ip", "-4", "-o", "addr", "show", "scope", "global").Output()
	if err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			fields := strings.Fields(line)
			if len(fields) >= 4 && fields[2] == "inet" {
				iface := fields[1]
				if strings.HasPrefix(iface, "lxc") || strings.HasPrefix(iface, "virbr") || strings.HasPrefix(iface, "docker") || strings.HasPrefix(iface, "veth") {
					continue
				}
				cidr := fields[3]
				ipPart := strings.Split(cidr, "/")[0]
				parsed := net.ParseIP(ipPart)
				if parsed != nil && parsed.To4() != nil && !parsed.IsLoopback() && !parsed.IsPrivate() {
					return parsed.String()
				}
			}
		}
	}
	return "host"
}

func getHostname() string {
	h, err := os.Hostname()
	if err != nil || h == "" {
		return "clicd-host"
	}
	return h
}

func getHostLoadSummary() string {
	out, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return "N/A"
	}
	fields := strings.Fields(string(out))
	if len(fields) >= 3 {
		return fmt.Sprintf("`%s, %s, %s`", fields[0], fields[1], fields[2])
	}
	return "N/A"
}

func getHostMemorySummary() string {
	out, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return "N/A"
	}
	var total, free, avail uint64
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			val, _ := strconv.ParseUint(fields[1], 10, 64)
			switch fields[0] {
			case "MemTotal:":
				total = val
			case "MemFree:":
				free = val
			case "MemAvailable:":
				avail = val
			}
		}
	}
	if avail == 0 {
		avail = free
	}
	used := total - avail
	usedGB := float64(used) / 1024 / 1024
	totalGB := float64(total) / 1024 / 1024
	pct := 0.0
	if total > 0 {
		pct = float64(used) / float64(total) * 100
	}
	return fmt.Sprintf("`%.1f%%` (`%.2f GB / %.2f GB`)", pct, usedGB, totalGB)
}

func getHostDiskSummary() string {
	var total, free uint64
	// Try df /
	out, err := exec.Command("df", "-B1", "/").Output()
	if err == nil {
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		if len(lines) >= 2 {
			fields := strings.Fields(lines[len(lines)-1])
			if len(fields) >= 4 {
				tot, _ := strconv.ParseUint(fields[1], 10, 64)
				usd, _ := strconv.ParseUint(fields[2], 10, 64)
				total = tot
				used := usd
				free = total - used
			}
		}
	}
	if total == 0 {
		return "N/A"
	}
	freeGB := float64(free) / 1024 / 1024 / 1024
	totalGB := float64(total) / 1024 / 1024 / 1024
	return fmt.Sprintf("`%.1f GB 可用 / %.1f GB 总量`", freeGB, totalGB)
}
