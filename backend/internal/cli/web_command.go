package cli

import (
	"fmt"
	"strings"

	"clicd/internal/config"
)

// RunWebCommand manages the web portal access toggle from CLI
func RunWebCommand(args []string) error {
	action := "status"
	if len(args) > 0 {
		action = strings.ToLower(strings.TrimSpace(args[0]))
	}

	switch action {
	case "status", "show", "info":
		if config.AppConfig.WebAccessDisabled {
			fmt.Println("Web 访问入口状态: 🔴 已关闭 (Disabled / 安全封锁)")
		} else {
			fmt.Println("Web 访问入口状态: 🟢 已开启 (Enabled / 正常访问)")
		}
		fmt.Printf("Web 面板端口: %d\n", config.AppConfig.Port)
		return nil

	case "on", "enable", "start", "open":
		config.AppConfig.WebAccessDisabled = false
		if err := config.SaveConfig(); err != nil {
			return fmt.Errorf("保存配置失败: %w", err)
		}
		fmt.Println("✅ Web 访问入口已成功【开启】！")
		return reloadPanelAfterAccessPolicyCommand()

	case "off", "disable", "stop", "close":
		config.AppConfig.WebAccessDisabled = true
		if err := config.SaveConfig(); err != nil {
			return fmt.Errorf("保存配置失败: %w", err)
		}
		fmt.Println("🔒 Web 访问入口已成功【关闭】(所有 Web 及 API 访问将被 404 拦截)！")
		return reloadPanelAfterAccessPolicyCommand()

	case "toggle":
		config.AppConfig.WebAccessDisabled = !config.AppConfig.WebAccessDisabled
		if err := config.SaveConfig(); err != nil {
			return fmt.Errorf("保存配置失败: %w", err)
		}
		if config.AppConfig.WebAccessDisabled {
			fmt.Println("🔒 Web 访问入口已切换为【关闭】！")
		} else {
			fmt.Println("✅ Web 访问入口已切换为【开启】！")
		}
		return reloadPanelAfterAccessPolicyCommand()

	default:
		return fmt.Errorf("未知指令 %q; 可用选项: clicd web status | clicd web on | clicd web off | clicd web toggle", action)
	}
}
