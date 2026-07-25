package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"clicd/internal/config"
	"clicd/internal/lxc"
	"clicd/internal/version"
)

var manager = lxc.NewManager()

const (
	clicdBackupDir              = "/root/clicd-backups"
	clicdNewBinaryPath          = "/usr/local/bin/clicd.new"
	libvirtDefaultNetworkMarker = "/var/lib/clicd/kvm/default-network.created"
)

var cliEnglish = detectCLIEnglish()

var cliTranslations = map[string]string{
	"重新加载配置失败":          "Failed to reload config",
	"请选择操作":             "Select an action",
	"再见":                "Goodbye",
	"无效选择":              "Invalid choice",
	"CLICD - LXC 容器管理器": "CLICD - Container Manager",
	"Web 面板":            "Web panel",
	"端口":                "port",
	"运行中":               "running",
	"已停止":               "stopped",
	"当前版本":              "Current version",
	"查看容器列表":            "List containers",
	"创建容器":              "Create container",
	"开机容器":              "Start container",
	"关机容器":              "Stop container",
	"重启容器":              "Restart container",
	"删除容器":              "Delete container",
	"重装容器系统":            "Reinstall container OS",
	"重置 Web 管理员密码":      "Reset web admin password",
	"启动":                "Start",
	"停止":                "Stop",
	"导入现有 LXC 容器":       "Import existing LXC containers",
	"检查并升级 CLICD":       "Check and upgrade CLICD",
	"卸载 CLICD":          "Uninstall CLICD",
	"面板访问白名单":           "Panel access allowlist",
	"系统信息":              "System info",
	"退出":                "Exit",
	"获取容器列表失败":          "Failed to get container list",
	"暂无容器":              "No containers",
	"容器":                "Container",
	"名称":                "Name",
	"状态":                "Status",
	"镜像":                "Image",
	"内存(MB)":            "Memory(MB)",
	"磁盘(GB)":            "Disk(GB)",
	"容器名称":              "Container name",
	"容器名称不能为空":          "Container name cannot be empty",
	"可用镜像":              "Available images",
	"镜像选择无效":            "Invalid image selection",
	"内存 (MB)":           "Memory (MB)",
	"磁盘 (GB)":           "Disk (GB)",
	"网络带宽 (Mbps)":       "Network bandwidth (Mbps)",
	"月流量 (GB)":          "Monthly traffic (GB)",
	"IO 速度 (MB/s)":      "IO speed (MB/s)",
	"额外 NAT 端口，多个用逗号分隔": "Extra NAT ports, comma-separated",
	"正在创建容器":            "Creating container",
	"创建失败":              "Create failed",
	"创建成功":              "created successfully",
	"端口未分配":             "port not assigned",
	"密码已保存，请在 Web 面板中查看或重置": "Password saved. View or reset it in the web panel",
	"开机失败":      "Start failed",
	"已开机":       "started",
	"关机失败":      "Stop failed",
	"已关机":       "stopped",
	"重启失败":      "Restart failed",
	"已重启":       "restarted",
	"开机":        "start",
	"关机":        "stop",
	"重启":        "restart",
	"删除":        "delete",
	"重装":        "reinstall",
	"确认删除容器":    "Delete container",
	"输入 yes 继续": "type yes to continue",
	"已取消":       "Cancelled",
	"删除失败":      "Delete failed",
	"已删除":       "deleted",
	"确认重装容器":    "Reinstall container",
	"重装失败":      "Reinstall failed",
	"已重装":       "reinstalled",
	"新的管理员密码（至少 6 位）": "New admin password (at least 6 characters)",
	"密码至少需要 6 位":      "Password must be at least 6 characters",
	"确认密码":            "Confirm password",
	"两次输入的密码不一致":      "Passwords do not match",
	"管理员密码已重置。":       "Admin password has been reset.",
	"按 Enter 返回菜单":    "Press Enter to return to menu",
	"选择要":             "Select a container to ",
	"的容器":             "",
	"选择无效":            "Invalid selection",
	"主机名":             "Hostname",
	"管理员用户":           "Admin user",
	"容器总数":            "Total containers",
	"切换语言":            "Switch language",
	"当前语言":            "Current language",
	"请选择语言":           "Select language",
	"语言已切换为":          "Language switched to",
	"保存语言失败":          "Failed to save language",
	"简体中文":            "Simplified Chinese",
	"重置失败":            "Reset failed",
	"停止 Web 面板失败":     "Failed to stop web panel",
	"Web 面板已停止，LXC 容器不会受影响。": "Web panel stopped. LXC containers are not affected.",
	"启动 Web 面板失败":            "Failed to start web panel",
	"Web 面板已启动":              "Web panel started",
	"升级只会替换 /usr/local/bin/clicd，并保留 /root/.clicd 里的配置、容器数据和任务记录。": "The upgrade only replaces /usr/local/bin/clicd and keeps configuration, container data, and task records under /root/.clicd.",
	"升级需要 root 权限。请使用: sudo clicd cli":                             "Upgrade requires root privileges. Use: sudo clicd cli",
	"检查仓库":             "Checking repository",
	"检查 GitHub 最新版本失败": "Failed to check the latest GitHub version",
	"GitHub Release 没有 tag_name，无法判断最新版本。": "GitHub Release has no tag_name, so the latest version cannot be determined.",
	"最新版本":            "Latest version",
	"发布页面":            "Release page",
	"当前架构不支持自动升级":     "Automatic upgrade is not supported on the current architecture",
	"最新 Release 没有找到": "The latest release does not contain",
	"无法自动升级。":         "automatic upgrade is unavailable.",
	"当前已经是最新版本。":      "The current version is already the latest.",
	"是否仍然重新安装最新版本？输入 reinstall 继续":          "Reinstall the latest version anyway? Type reinstall to continue",
	"输入 upgrade 开始升级":                       "Type upgrade to start upgrade",
	"已取消。":                                  "Cancelled.",
	"升级失败":                                  "Upgrade failed",
	"升级完成":                                  "Upgrade completed",
	"原有数据已保留，Web 服务已重启。":                    "Existing data has been kept and the web service has been restarted.",
	"GitHub API 返回":                         "GitHub API returned",
	"GitHub API 被限流，已切换到备用检查方式。":            "GitHub API rate limit reached; switched to fallback check.",
	"GitHub API 不可用，已切换到备用检查方式。":            "GitHub API is unavailable; switched to fallback check.",
	"GitHub releases/latest 返回":             "GitHub releases/latest returned",
	"无法从 GitHub releases/latest 跳转结果解析最新版本": "Unable to parse the latest version from the GitHub releases/latest redirect",
	"正在下载升级包...":                            "Downloading upgrade package...",
	"正在解压升级包...":                            "Extracting upgrade package...",
	"解压失败":                                  "Extraction failed",
	"备份旧二进制失败":                              "Failed to back up old binary",
	"旧版本已备份":                                "Old version backed up",
	"正在替换二进制...":                            "Replacing binary...",
	"停止 Web 服务失败，继续尝试替换":                    "Failed to stop web service; continuing replacement attempt",
	"二进制已替换，但重启 Web 服务失败":                   "Binary was replaced, but restarting the web service failed",
	"下载失败，HTTP":                             "Download failed, HTTP",
	"升级包内未找到 clicd 二进制":                     "No clicd binary found in the upgrade package",
	"将 /var/lib/lxc 里的容器导入 CLICD 配置。":       "Import containers under /var/lib/lxc into CLICD configuration.",
	"导入后会保留真实 LXC 名称，Web 和 CLI 都能管理同一个容器。": "After import, real LXC names are kept and both Web and CLI can manage the same containers.",
	"导入失败":            "Import failed",
	"没有发现新的 ct-* 容器。": "No new ct-* containers found.",
	"已导入":             "Imported",
	"个容器":             "containers",
	"将删除 CLICD 服务和 /usr/local/bin/clicd。": "This will remove the CLICD service and /usr/local/bin/clicd.",
	"同时会删除 /root/.clicd、/var/lib/lxc、/var/lib/clicd、镜像缓存、备份、临时文件、/swapfile 和 CLICD 网络规则。": "It will also remove /root/.clicd, /var/lib/lxc, /var/lib/clicd, image caches, backups, temporary files, /swapfile, and CLICD network rules.",
	"卸载需要 root 权限。":                "Uninstall requires root privileges.",
	"请运行: sudo clicd cli --no-web": "Run: sudo clicd cli --no-web",
	"输入 uninstall 继续卸载":            "Type uninstall to continue uninstalling",
	"CLICD 已卸载。":                   "CLICD has been uninstalled.",
	"服务、二进制、配置、容器/虚拟机、本地镜像、缓存、备份、临时文件和 CLICD 网络规则均已删除。":         "Service, binary, configuration, containers/VMs, local images, cache, backups, temporary files, and CLICD network rules have been removed.",
	"检测到非 CLICD 虚拟机仍在使用 libvirt default 网络，已保留 default/virbr0。": "Non-CLICD VMs are still using the libvirt default network, so default/virbr0 has been kept.",
	"Web 面板重载跳过":        "Web panel reload skipped",
	"Web 面板已重载并应用配置变更。": "Web panel reloaded and configuration changes applied.",
	"读取容器状态失败":          "Failed to read container status",
	"CLICD 版本":          "CLICD version",
	"Web 端口":            "Web port",
	"LXC 版本":            "LXC version",
	"暂无可用容器":            "No available containers",
	"忽略无效端口":            "Ignoring invalid port",
	"面板访问来源策略":          "Panel access source policy",
	"当前状态":              "Current status",
	"已启用":               "enabled",
	"已关闭":               "disabled",
	"允许来源":              "Allowed sources",
	"可信代理":              "Trusted proxies",
	"启用或修改白名单":          "Enable or update allowlist",
	"关闭白名单限制":           "Disable allowlist",
	"取消":                "Cancel",
	"允许的 IP/CIDR，多个用逗号分隔":      "Allowed IP/CIDR values, comma-separated",
	"可信代理 IP/CIDR，多个用逗号分隔，可留空": "Trusted proxy IP/CIDR values, comma-separated; optional",
	"白名单配置无效":                  "Invalid allowlist configuration",
	"保存访问来源策略失败":               "Failed to save access source policy",
	"面板访问白名单已保存。":              "Panel access allowlist saved.",
	"面板访问白名单已关闭。":              "Panel access allowlist disabled.",
	"至少填写一个允许的 IP 或网段。":        "Enter at least one allowed IP address or network.",
	"？": "? ",
	"。": ". ",
	"，": ", ",
	"：": ": ",
}

// Run starts the CLI interface.
func Run() {
	reader := bufio.NewReader(os.Stdin)

	for {
		if _, err := config.InitConfig(); err != nil {
			cliPrintf("重新加载配置失败: %v\n", err)
			waitEnter(reader)
		}
		refreshCLILanguage()
		clearScreen()
		printMenu()
		cliPrint("\n请选择操作 [1-13,l,0/q]: ")
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)

		switch strings.ToLower(input) {
		case "1":
			clearScreen()
			cliListContainers()
			waitEnter(reader)
		case "2":
			clearScreen()
			cliCreateContainer(reader)
			waitEnter(reader)
		case "3":
			clearScreen()
			cliStartContainer(reader)
			waitEnter(reader)
		case "4":
			clearScreen()
			cliStopContainer(reader)
			waitEnter(reader)
		case "5":
			clearScreen()
			cliRestartContainer(reader)
			waitEnter(reader)
		case "6":
			clearScreen()
			cliDeleteContainer(reader)
			waitEnter(reader)
		case "7":
			clearScreen()
			cliReinstallContainer(reader)
			waitEnter(reader)
		case "8":
			clearScreen()
			cliResetPassword(reader)
			waitEnter(reader)
		case "9":
			clearScreen()
			cliToggleWebPanel()
			waitEnter(reader)
		case "10":
			clearScreen()
			cliImportExistingContainers()
			waitEnter(reader)
		case "11":
			clearScreen()
			cliUpgradeSystem(reader)
			waitEnter(reader)
		case "12":
			clearScreen()
			cliUninstall(reader)
			return
		case "13":
			clearScreen()
			cliConfigurePanelAccess(reader)
			waitEnter(reader)
		case "0":
			clearScreen()
			cliShowInfo()
			waitEnter(reader)
		case "l", "lang", "language":
			clearScreen()
			cliSwitchLanguage(reader)
			waitEnter(reader)
		case "q", "exit", "quit":
			cliPrintln("再见")
			return
		default:
			cliPrintln("无效选择")
		}
	}
}

func printMenu() {
	webStatus := "启动"
	if isWebPanelRunning() {
		webStatus = "停止"
	}
	cliPrintln("")
	cliPrintln("  ==========================================")
	cliPrintln("       CLICD - LXC 容器管理器")
	cliPrintln("  ==========================================")
	cliPrintln("")
	cliPrintf("  Web 面板: %s (端口 %d)\n", func() string {
		if isWebPanelRunning() {
			return "运行中"
		}
		return "已停止"
	}(), config.AppConfig.Port)
	cliPrintf("  当前版本: %s\n", version.Current())
	cliPrintln("")
	cliPrintln("  1. 查看容器列表")
	cliPrintln("  2. 创建容器")
	cliPrintln("  3. 开机容器")
	cliPrintln("  4. 关机容器")
	cliPrintln("  5. 重启容器")
	cliPrintln("  6. 删除容器")
	cliPrintln("  7. 重装容器系统")
	cliPrintln("  8. 重置 Web 管理员密码")
	cliPrintf("  9. %s Web 面板\n", webStatus)
	cliPrintln("  10. 导入现有 LXC 容器")
	cliPrintln("  11. 检查并升级 CLICD")
	cliPrintln("  12. 卸载 CLICD")
	cliPrintln("  13. 面板访问白名单")
	cliPrintln("  0. 系统信息")
	cliPrintln("  l. 切换语言")
	cliPrintln("  q. 退出")
}

func cliConfigurePanelAccess(reader *bufio.Reader) {
	cliPrintf("\n--- %s ---\n", cliT("面板访问来源策略"))
	policy := config.AppConfig.PanelAccessPolicy
	status := cliT("已关闭")
	if policy.Enabled {
		status = cliT("已启用")
	}
	cliPrintf("%s: %s\n", cliT("当前状态"), status)
	cliPrintf("%s: %s\n", cliT("允许来源"), strings.Join(policy.AllowedSources, ", "))
	cliPrintf("%s: %s\n", cliT("可信代理"), strings.Join(policy.TrustedProxies, ", "))
	cliPrintf("\n  1. %s\n", cliT("启用或修改白名单"))
	cliPrintf("  2. %s\n", cliT("关闭白名单限制"))
	cliPrintf("  0. %s\n", cliT("取消"))

	choice := promptString(reader, "请选择操作", "0")
	next := policy
	switch strings.TrimSpace(choice) {
	case "1":
		allowed := promptString(reader, "允许的 IP/CIDR，多个用逗号分隔", strings.Join(policy.AllowedSources, ","))
		allowedSources := splitPanelAccessEntries(allowed)
		if len(allowedSources) == 0 {
			cliPrintln("至少填写一个允许的 IP 或网段。")
			return
		}
		trusted := promptString(reader, "可信代理 IP/CIDR，多个用逗号分隔，可留空", strings.Join(policy.TrustedProxies, ","))
		next = config.PanelAccessPolicy{
			Enabled:        true,
			AllowedSources: allowedSources,
			TrustedProxies: splitPanelAccessEntries(trusted),
		}
	case "2":
		next.Enabled = false
	case "0", "":
		cliPrintln("已取消")
		return
	default:
		cliPrintln("无效选择")
		return
	}

	normalized, err := config.NormalizePanelAccessPolicy(next)
	if err != nil {
		cliPrintf("%s: %v\n", cliT("白名单配置无效"), err)
		return
	}
	previous := config.AppConfig.PanelAccessPolicy
	config.AppConfig.PanelAccessPolicy = normalized
	if err := config.SaveConfig(); err != nil {
		config.AppConfig.PanelAccessPolicy = previous
		cliPrintf("%s: %v\n", cliT("保存访问来源策略失败"), err)
		return
	}
	if normalized.Enabled {
		cliPrintln("面板访问白名单已保存。")
	} else {
		cliPrintln("面板访问白名单已关闭。")
	}
	if isWebPanelRunning() {
		restartWebPanelForConfigChange()
	}
}

func splitPanelAccessEntries(value string) []string {
	return strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n' || r == '\r' || r == '\t' || r == ' '
	})
}

func cliSwitchLanguage(reader *bufio.Reader) {
	cliPrintf("\n--- %s ---\n", cliT("切换语言"))
	cliPrintf("%s: %s\n", cliT("当前语言"), cliLanguageLabel(config.NormalizeLanguage(config.AppConfig.Language)))
	cliPrintln("  1. 简体中文")
	cliPrintln("  2. English")
	choice := promptString(reader, "请选择语言 [1/2]", func() string {
		if config.NormalizeLanguage(config.AppConfig.Language) == "en" {
			return "2"
		}
		return "1"
	}())

	next := "zh"
	switch strings.ToLower(strings.TrimSpace(choice)) {
	case "2", "en", "english":
		next = "en"
	case "1", "zh", "cn", "chinese":
		next = "zh"
	default:
		cliPrintln("无效选择")
		return
	}

	config.AppConfig.Language = next
	if err := config.SaveConfig(); err != nil {
		cliPrintf("保存语言失败: %v\n", err)
		return
	}
	_ = os.Setenv("CLICD_LANG", next)
	refreshCLILanguage()
	cliPrintf("%s: %s\n", cliT("语言已切换为"), cliLanguageLabel(next))
	if isWebPanelRunning() {
		restartWebPanelForConfigChange()
	}
}

func cliListContainers() {
	containers, err := manager.ListContainers()
	if err != nil {
		cliPrintf("获取容器列表失败: %v\n", err)
		return
	}

	if len(containers) == 0 {
		cliPrintln("\n暂无容器")
		return
	}

	fmt.Println()
	fmt.Printf("%-18s %-10s %-18s %-6s %-10s %-10s %-16s\n", cliT("名称"), cliT("状态"), cliT("镜像"), "vCPU", cliT("内存(MB)"), cliT("磁盘(GB)"), "SSH")
	fmt.Println(strings.Repeat("-", 94))
	for _, c := range containers {
		ssh := "-"
		if c.SSHPort > 0 {
			ssh = fmt.Sprintf("%d->22", c.SSHPort)
		}
		fmt.Printf("%-18s %-10s %-18s %-6.2f %-10d %-10d %-16s\n",
			c.Name, c.Status, c.Template, c.VCPU, c.RAMMB, c.DiskGB, ssh)
	}
}

func cliCreateContainer(reader *bufio.Reader) {
	cliPrintln("\n--- 创建容器 ---")

	name := promptString(reader, "容器名称", "")
	if name == "" {
		cliPrintln("容器名称不能为空")
		return
	}

	templates := lxc.GetTemplates()
	cliPrintln("\n可用镜像:")
	for i, template := range templates {
		fmt.Printf("  %d. %s\n", i+1, template.Name)
	}

	tmplIdx := promptInt(reader, fmt.Sprintf("镜像 [1-%d]", len(templates)), 1)
	if tmplIdx < 1 || tmplIdx > len(templates) {
		cliPrintln("镜像选择无效")
		return
	}

	cfg := lxc.ContainerConfig{
		Name:             name,
		TemplateID:       templates[tmplIdx-1].ID,
		VCPU:             promptFloat(reader, "vCPU", 1),
		RAMMB:            promptInt(reader, "内存 (MB)", 512),
		DiskGB:           promptInt(reader, "磁盘 (GB)", 10),
		NetworkBWMbps:    promptInt(reader, "网络带宽 (Mbps)", 100),
		MonthlyTrafficGB: promptInt(reader, "月流量 (GB)", 1000),
		IOSpeedMBps:      promptInt(reader, "IO 速度 (MB/s)", 500),
		ExtraPorts:       promptPortList(reader, "额外 NAT 端口，多个用逗号分隔"),
	}
	cfg.NormalizeResourceAliases()

	cliPrintf("\n正在创建容器 %s ...\n", name)
	if err := manager.CreateContainer(cfg); err != nil {
		cliPrintf("创建失败: %v\n", err)
		return
	}

	container := config.FindContainerByName(name)
	cliPrintf("容器 %s 创建成功\n", name)
	if container != nil {
		cliPrint(formatSSHAccess(container.SSHPort))
	}
	restartWebPanelForConfigChange()
}

func formatSSHAccess(sshPort int) string {
	if sshPort <= 0 {
		return "SSH: root, 端口未分配。密码已保存，请在 Web 面板中查看或重置。\n"
	}
	return fmt.Sprintf("SSH: root, port %d -> 22。密码已保存，请在 Web 面板中查看或重置。\n", sshPort)
}

func cliStartContainer(reader *bufio.Reader) {
	id, name := selectContainer(reader, "开机")
	if id == 0 {
		return
	}
	if err := manager.StartContainer(id); err != nil {
		cliPrintf("开机失败: %v\n", err)
		return
	}
	cliPrintf("容器 %s 已开机\n", name)
}

func cliStopContainer(reader *bufio.Reader) {
	id, name := selectContainer(reader, "关机")
	if id == 0 {
		return
	}
	if err := manager.StopContainer(id); err != nil {
		cliPrintf("关机失败: %v\n", err)
		return
	}
	cliPrintf("容器 %s 已关机\n", name)
}

func cliRestartContainer(reader *bufio.Reader) {
	id, name := selectContainer(reader, "重启")
	if id == 0 {
		return
	}
	if err := manager.RestartContainer(id); err != nil {
		cliPrintf("重启失败: %v\n", err)
		return
	}
	cliPrintf("容器 %s 已重启\n", name)
}

func cliDeleteContainer(reader *bufio.Reader) {
	id, name := selectContainer(reader, "删除")
	if id == 0 {
		return
	}
	confirm := promptString(reader, fmt.Sprintf("确认删除容器 %s？输入 yes 继续", name), "no")
	if strings.ToLower(confirm) != "yes" {
		cliPrintln("已取消")
		return
	}
	if err := manager.DestroyContainer(id); err != nil {
		cliPrintf("删除失败: %v\n", err)
		return
	}
	cliPrintf("容器 %s 已删除\n", name)
	restartWebPanelForConfigChange()
}

func cliReinstallContainer(reader *bufio.Reader) {
	id, name := selectContainer(reader, "重装")
	if id == 0 {
		return
	}

	templates := lxc.GetTemplates()
	cliPrintln("\n可用镜像:")
	for i, template := range templates {
		fmt.Printf("  %d. %s\n", i+1, template.Name)
	}

	tmplIdx := promptInt(reader, fmt.Sprintf("镜像 [1-%d]", len(templates)), 1)
	if tmplIdx < 1 || tmplIdx > len(templates) {
		cliPrintln("镜像选择无效")
		return
	}

	confirm := promptString(reader, fmt.Sprintf("确认重装容器 %s？输入 yes 继续", name), "no")
	if strings.ToLower(confirm) != "yes" {
		cliPrintln("已取消")
		return
	}

	if err := manager.ReinstallContainer(id, templates[tmplIdx-1].ID); err != nil {
		cliPrintf("重装失败: %v\n", err)
		return
	}
	cliPrintf("容器 %s 已重装\n", name)
	restartWebPanelForConfigChange()
}

func cliResetPassword(reader *bufio.Reader) {
	newPass := promptString(reader, "新的管理员密码（至少 6 位）", "")
	if len(newPass) < 6 {
		cliPrintln("密码至少需要 6 位")
		return
	}
	confirm := promptString(reader, "确认密码", "")
	if newPass != confirm {
		cliPrintln("两次输入的密码不一致")
		return
	}

	if err := config.ResetAdminPassword(newPass); err != nil {
		cliPrintf("重置失败: %v\n", err)
		return
	}
	cliPrintln("管理员密码已重置。")
	restartWebPanelForConfigChange()
}

func cliToggleWebPanel() {
	if isWebPanelRunning() {
		if err := stopService("clicd"); err != nil {
			cliPrintf("停止 Web 面板失败: %v\n", err)
			return
		}
		cliPrintln("Web 面板已停止，LXC 容器不会受影响。")
		return
	}

	if err := startService("clicd"); err != nil {
		cliPrintf("启动 Web 面板失败: %v\n", err)
		return
	}
	cliPrintln("Web 面板已启动")
}

type githubRelease struct {
	TagName string `json:"tag_name"`
	Name    string `json:"name"`
	HTMLURL string `json:"html_url"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

func cliUpgradeSystem(reader *bufio.Reader) {
	cliPrintln("\n--- 检查并升级 CLICD ---")
	cliPrintln("升级只会替换 /usr/local/bin/clicd，并保留 /root/.clicd 里的配置、容器数据和任务记录。")

	if os.Geteuid() != 0 {
		cliPrintln("升级需要 root 权限。请使用: sudo clicd cli")
		return
	}

	repo := strings.TrimSpace(os.Getenv("CLICD_REPO"))
	if repo == "" {
		repo = version.Repo
	}
	assetName, err := releaseArchiveAssetName(runtime.GOARCH)
	if err != nil {
		cliPrintf("当前架构不支持自动升级: %s\n", runtime.GOARCH)
		return
	}
	current := version.Current()
	cliPrintf("当前版本: %s\n", current)
	cliPrintf("检查仓库: https://github.com/%s\n", repo)

	release, err := fetchLatestRelease(repo, assetName)
	if err != nil {
		cliPrintf("检查 GitHub 最新版本失败: %v\n", err)
		return
	}
	latest := strings.TrimSpace(release.TagName)
	if latest == "" {
		cliPrintln("GitHub Release 没有 tag_name，无法判断最新版本。")
		return
	}
	cliPrintf("最新版本: %s\n", latest)
	if release.HTMLURL != "" {
		cliPrintf("发布页面: %s\n", release.HTMLURL)
	}

	assetURL := findReleaseAsset(release, assetName)
	if assetURL == "" {
		cliPrintf("最新 Release 没有找到 %s，无法自动升级。\n", assetName)
		return
	}

	if sameVersion(current, latest) {
		cliPrintln("当前已经是最新版本。")
		confirm := promptString(reader, "是否仍然重新安装最新版本？输入 reinstall 继续", "no")
		if strings.ToLower(confirm) != "reinstall" {
			cliPrintln("已取消。")
			return
		}
	} else {
		confirm := promptString(reader, "输入 upgrade 开始升级", "no")
		if strings.ToLower(confirm) != "upgrade" {
			cliPrintln("已取消。")
			return
		}
	}

	if err := upgradeFromReleaseAsset(assetURL, latest, assetName); err != nil {
		cliPrintf("升级失败: %v\n", err)
		return
	}
	cliPrintf("升级完成: %s -> %s\n", current, latest)
	cliPrintln("原有数据已保留，Web 服务已重启。")
}

func fetchLatestRelease(repo, assetName string) (*githubRelease, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	setGitHubRequestHeaders(req)

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		if fallback, fallbackErr := fetchLatestReleaseFallback(repo, assetName); fallbackErr == nil {
			return fallback, nil
		}
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		apiErr := fmt.Errorf("GitHub API 返回 %s: %s", resp.Status, strings.TrimSpace(string(body)))
		if fallback, fallbackErr := fetchLatestReleaseFallback(repo, assetName); fallbackErr == nil {
			if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
				cliPrintln("GitHub API 被限流，已切换到备用检查方式。")
			} else {
				cliPrintln("GitHub API 不可用，已切换到备用检查方式。")
			}
			return fallback, nil
		}
		return nil, apiErr
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, err
	}
	return &release, nil
}

func fetchLatestReleaseFallback(repo, assetName string) (*githubRelease, error) {
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("https://github.com/%s/releases/latest", repo), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "clicd-updater/"+version.Current())

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("GitHub releases/latest 返回 %s", resp.Status)
	}

	tag := latestTagFromPath(resp.Request.URL.Path)
	if tag == "" {
		return nil, fmt.Errorf("无法从 GitHub releases/latest 跳转结果解析最新版本")
	}

	return &githubRelease{
		TagName: tag,
		Name:    tag,
		HTMLURL: fmt.Sprintf("https://github.com/%s/releases/tag/%s", repo, tag),
		Assets: []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		}{
			{
				Name:               assetName,
				BrowserDownloadURL: fmt.Sprintf("https://github.com/%s/releases/latest/download/%s", repo, assetName),
			},
		},
	}, nil
}

func latestTagFromPath(path string) string {
	const marker = "/releases/tag/"
	idx := strings.Index(path, marker)
	if idx < 0 {
		return ""
	}
	tag := strings.TrimSpace(path[idx+len(marker):])
	if slash := strings.Index(tag, "/"); slash >= 0 {
		tag = tag[:slash]
	}
	return tag
}

func setGitHubRequestHeaders(req *http.Request) {
	req.Header.Set("User-Agent", "clicd-updater/"+version.Current())
	token := strings.TrimSpace(os.Getenv("CLICD_GITHUB_TOKEN"))
	if token == "" {
		token = strings.TrimSpace(os.Getenv("GITHUB_TOKEN"))
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
}

func releaseArchiveAssetName(goarch string) (string, error) {
	switch goarch {
	case "amd64", "arm64":
		return fmt.Sprintf("clicd-linux-%s.tar.gz", goarch), nil
	default:
		return "", fmt.Errorf("unsupported architecture: %s", goarch)
	}
}

func findReleaseAsset(release *githubRelease, name string) string {
	for _, asset := range release.Assets {
		if asset.Name == name && asset.BrowserDownloadURL != "" {
			return asset.BrowserDownloadURL
		}
	}
	return ""
}

func upgradeFromReleaseAsset(assetURL, latest, assetName string) error {
	tmpDir, err := os.MkdirTemp("", "clicd-upgrade-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	archivePath := filepath.Join(tmpDir, assetName)
	cliPrintln("正在下载升级包...")
	if err := downloadFile(assetURL, archivePath); err != nil {
		return err
	}

	cliPrintln("正在解压升级包...")
	if out, err := exec.Command("tar", "-xzf", archivePath, "-C", tmpDir).CombinedOutput(); err != nil {
		return fmt.Errorf("解压失败: %v, output: %s", err, string(out))
	}

	newBinary, err := findFile(tmpDir, "clicd")
	if err != nil {
		return err
	}

	backupDir := clicdBackupDir
	if err := os.MkdirAll(backupDir, 0700); err != nil {
		return err
	}
	backupName := fmt.Sprintf("clicd.%s.%s", safeReleaseBackupComponent(latest), time.Now().Format("20060102-150405"))
	if _, err := os.Stat("/usr/local/bin/clicd"); err == nil {
		backupPath, err := copyFileToBackup("/usr/local/bin/clicd", backupName, 0755)
		if err != nil {
			return fmt.Errorf("备份旧二进制失败: %w", err)
		}
		cliPrintf("旧版本已备份: %s\n", backupPath)
	}

	cliPrintln("正在替换二进制...")
	if err := stopService("clicd"); err != nil {
		cliPrintf("停止 Web 服务失败，继续尝试替换: %v\n", err)
	}
	tmpBin := clicdNewBinaryPath
	if err := copyFileToUpgradeTemp(newBinary, 0755); err != nil {
		return err
	}
	if err := os.Rename(tmpBin, "/usr/local/bin/clicd"); err != nil {
		return err
	}
	if err := os.Chmod("/usr/local/bin/clicd", 0755); err != nil {
		return err
	}

	if err := restartService("clicd"); err != nil {
		return fmt.Errorf("二进制已替换，但重启 Web 服务失败: %w", err)
	}
	return nil
}

func downloadFile(url, dest string) error {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	setGitHubRequestHeaders(req)
	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载失败，HTTP %s", resp.Status)
	}

	out, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, resp.Body)
	return err
}

func findFile(root, name string) (string, error) {
	var found string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if d.Name() == name {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if found == "" {
		return "", fmt.Errorf("升级包内未找到 clicd 二进制")
	}
	return found, nil
}

func copyFileToBackup(src, fileName string, mode os.FileMode) (string, error) {
	if fileName == "" || strings.Contains(fileName, "/") || strings.Contains(fileName, "\\") || strings.Contains(fileName, "..") {
		return "", fmt.Errorf("unsafe backup file name: %s", fileName)
	}
	dst := filepath.Join(clicdBackupDir, fileName)
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return "", err
	}
	if err := copyIntoOpenFile(src, out, mode); err != nil {
		return "", err
	}
	return dst, nil
}

func copyFileToUpgradeTemp(src string, mode os.FileMode) error {
	out, err := os.OpenFile(clicdNewBinaryPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	return copyIntoOpenFile(src, out, mode)
}

func copyIntoOpenFile(src string, out *os.File, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		out.Close()
		return err
	}
	defer in.Close()

	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Chmod(mode); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return nil
}

func safeReleaseBackupComponent(tag string) string {
	tag = strings.TrimPrefix(strings.TrimSpace(tag), "v")
	var b strings.Builder
	for _, r := range tag {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('_')
	}
	component := strings.Trim(b.String(), "._-")
	if component == "" {
		return "unknown"
	}
	if len(component) > 64 {
		return component[:64]
	}
	return component
}

func sameVersion(current, latest string) bool {
	c := strings.TrimPrefix(strings.TrimSpace(strings.ToLower(current)), "v")
	l := strings.TrimPrefix(strings.TrimSpace(strings.ToLower(latest)), "v")
	return c != "" && c == l
}

func isWebPanelRunning() bool {
	if commandExists("systemctl") {
		cmd := exec.Command("systemctl", "is-active", "clicd")
		output, err := cmd.Output()
		if err == nil && strings.TrimSpace(string(output)) == "active" {
			return true
		}
	}
	if commandExists("rc-service") {
		cmd := exec.Command("rc-service", "clicd", "status")
		return cmd.Run() == nil
	}
	return false
}

func cliImportExistingContainers() {
	cliPrintln("\n--- 导入现有 LXC 容器 ---")
	cliPrintln("将 /var/lib/lxc 里的容器导入 CLICD 配置。")
	cliPrintln("导入后会保留真实 LXC 名称，Web 和 CLI 都能管理同一个容器。")

	imported, err := manager.ImportExistingClicdContainers()
	if err != nil {
		cliPrintf("导入失败: %v\n", err)
		return
	}
	if len(imported) == 0 {
		cliPrintln("没有发现新的 ct-* 容器。")
		return
	}

	cliPrintf("已导入 %d 个容器:\n", len(imported))
	for _, c := range imported {
		fmt.Printf("  [%d] %s [%s]\n", c.ID, c.Name, c.Status)
	}
	restartWebPanelForConfigChange()
}

func cliUninstall(reader *bufio.Reader) {
	cliPrintln("\n--- 卸载 CLICD ---")
	cliPrintln("将删除 CLICD 服务和 /usr/local/bin/clicd。")
	cliPrintln("同时会删除 /root/.clicd、/var/lib/lxc、/var/lib/clicd、镜像缓存、备份、临时文件、/swapfile 和 CLICD 网络规则。")

	if os.Geteuid() != 0 {
		cliPrintln("卸载需要 root 权限。")
		cliPrintln("请运行: sudo clicd cli --no-web")
		return
	}

	confirm := promptString(reader, "输入 uninstall 继续卸载", "no")
	if strings.ToLower(confirm) != "uninstall" {
		cliPrintln("已取消")
		return
	}

	destroyAllLXCContainers()
	destroyAllKVMDomains()
	removeCLICDLibvirtDefaultNetwork()
	cleanupCLICDNetworking()
	removeCLICDHostHooks()
	removeCLICDQuotaRecords()
	stopAndRemoveService()
	removePath("/usr/local/bin/clicd")
	removePath("/etc/sysctl.d/99-clicd.conf")
	removePath("/var/log/clicd.log")
	removePath("/var/log/clicd.err")
	removePath("/root/.clicd")
	removePath("/var/lib/lxc")
	removePath("/var/lib/clicd")
	removePath("/var/cache/lxc")
	removePath("/var/cache/clicd")
	removePath("/root/clicd-backups")
	removeCLICDTmpFiles()
	removeCLICDSwapfile()

	reloadSysctl()

	fmt.Println()
	cliPrintln("CLICD 已卸载。")
	cliPrintln("服务、二进制、配置、容器/虚拟机、本地镜像、缓存、备份、临时文件和 CLICD 网络规则均已删除。")
}

func destroyAllLXCContainers() {
	entries, err := os.ReadDir("/var/lib/lxc")
	if err != nil {
		return
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		fmt.Printf("Destroying LXC container %s...\n", name)
		runQuiet("lxc-stop", "-n", name, "-k")
		runQuiet("lxc-destroy", "-n", name, "-f")
		removeLXCContainerPath("/var/lib/lxc/" + name)
	}
}

func destroyAllKVMDomains() {
	if !commandExists("virsh") {
		return
	}
	out, err := exec.Command("virsh", "list", "--all", "--name").Output()
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(out), "\n") {
		name := strings.TrimSpace(line)
		if isCLICDKVMDomain(name) {
			removeKVMDomain(name)
		}
	}
}

func isCLICDKVMDomain(name string) bool {
	if !strings.HasPrefix(name, "vm-") || len(name) <= len("vm-") {
		return false
	}
	for _, r := range strings.TrimPrefix(name, "vm-") {
		if r < '0' || r > '9' {
			return false
		}
	}
	if dirExists("/var/lib/clicd/kvm/instances/" + name) {
		return true
	}
	out, err := exec.Command("virsh", "dumpxml", name).Output()
	return err == nil && strings.Contains(string(out), "/var/lib/clicd/kvm/")
}

func removeKVMDomain(name string) {
	fmt.Printf("Removing KVM domain %s...\n", name)
	runQuiet("virsh", "destroy", name)
	if runCommandOK("virsh", "undefine", name, "--remove-all-storage", "--nvram") {
		return
	}
	if runCommandOK("virsh", "undefine", name, "--nvram") {
		return
	}
	runQuiet("virsh", "undefine", name)
}

func removeCLICDLibvirtDefaultNetwork() {
	if !commandExists("virsh") || !fileExists(libvirtDefaultNetworkMarker) {
		return
	}
	if libvirtDefaultUsedByNonCLICDDomain() {
		cliPrintln("检测到非 CLICD 虚拟机仍在使用 libvirt default 网络，已保留 default/virbr0。")
		return
	}
	fmt.Println("Removing CLICD-created libvirt default network...")
	runQuiet("virsh", "net-destroy", "default")
	runQuiet("virsh", "net-undefine", "default")
	removePath(libvirtDefaultNetworkMarker)
}

func libvirtDefaultUsedByNonCLICDDomain() bool {
	if !commandExists("virsh") {
		return false
	}
	out, err := exec.Command("virsh", "list", "--all", "--name").Output()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(out), "\n") {
		name := strings.TrimSpace(line)
		if name == "" || isCLICDKVMDomain(name) {
			continue
		}
		if usesLibvirtDefaultNetwork(name) {
			return true
		}
	}
	return false
}

func usesLibvirtDefaultNetwork(domain string) bool {
	out, err := exec.Command("virsh", "domiflist", domain).Output()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		for _, field := range fields {
			if field == "default" || field == "virbr0" {
				return true
			}
		}
	}
	return false
}

func cleanupCLICDNetworking() {
	removeCLICDNATRules()
	cleanupCLICDIPv6Runtime()
	cleanupCLICDIPv6BridgeRoutes()
	for _, bridge := range []string{"lxcbr0", "virbr0"} {
		deleteFilterRule("FORWARD", "-i", bridge, "-j", "ACCEPT")
		deleteFilterRule("FORWARD", "-o", bridge, "-j", "ACCEPT")
		deleteFilterRule("FORWARD", "-i", bridge, "-o", bridge, "-j", "ACCEPT")
		deleteIP6TablesBridgeRules(bridge)
	}
}

func cleanupCLICDIPv6Runtime() {
	if config.AppConfig == nil {
		return
	}
	for _, c := range config.AppConfig.Containers {
		cleanupCLICDContainerIPv6(c)
	}
}

func cleanupCLICDContainerIPv6(c config.Container) {
	bridge := "lxcbr0"
	if c.IsKVM() {
		bridge = "virbr0"
	}
	mac := strings.ToLower(strings.TrimSpace(c.MACAddress))
	if mac != "" && bridge == "virbr0" {
		deleteIP6FilterRule("FORWARD", "-i", bridge, "-m", "mac", "--mac-source", mac, "-j", "DROP")
	}
	if strings.TrimSpace(c.IPv6) == "" {
		return
	}

	addr := strings.TrimSpace(c.IPv6)
	if slash := strings.Index(addr, "/"); slash >= 0 {
		addr = addr[:slash]
	}
	source := strings.TrimSpace(c.IPv6)
	if !strings.Contains(source, "/") {
		source += "/128"
	}

	deleteIP6NATSource(source)
	deleteIP6FilterRule("FORWARD", "-i", bridge, "-s", source, "-j", "ACCEPT")
	deleteIP6FilterRule("FORWARD", "-o", bridge, "-d", source, "-j", "ACCEPT")
	if mac != "" && bridge == "virbr0" {
		deleteIP6FilterRule("FORWARD", "-i", bridge, "-m", "mac", "--mac-source", mac, "-s", source, "-j", "ACCEPT")
		deleteIP6FilterRule("FORWARD", "-i", bridge, "-m", "mac", "--mac-source", mac, "-j", "DROP")
	}

	runQuiet("ip", "-6", "route", "del", source, "dev", bridge)
	if strings.TrimSpace(c.IPv6Interface) != "" {
		runQuiet("ip", "-6", "neigh", "del", "proxy", addr, "dev", c.IPv6Interface)
	}
}

func cleanupCLICDIPv6BridgeRoutes() {
	if !commandExists("ip") {
		return
	}
	for _, bridge := range []string{"lxcbr0", "virbr0"} {
		out, err := exec.Command("ip", "-6", "route", "show", "dev", bridge).Output()
		if err == nil {
			for _, line := range strings.Split(string(out), "\n") {
				fields := strings.Fields(line)
				if len(fields) == 0 || !strings.HasSuffix(fields[0], "/128") {
					continue
				}
				source := fields[0]
				addr := strings.TrimSuffix(source, "/128")
				deleteIP6NATSource(source)
				deleteIP6FilterRule("FORWARD", "-i", bridge, "-s", source, "-j", "ACCEPT")
				deleteIP6FilterRule("FORWARD", "-o", bridge, "-d", source, "-j", "ACCEPT")
				removeProxyNDPForAddress(addr)
				runQuiet("ip", "-6", "route", "del", source, "dev", bridge)
			}
		}
		runQuiet("ip", "-6", "addr", "del", "fe80::1/64", "dev", bridge)
	}
}

func removeProxyNDPForAddress(addr string) {
	out, err := exec.Command("ip", "-6", "neigh", "show", "proxy").Output()
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] != addr {
			continue
		}
		for i := 0; i+1 < len(fields); i++ {
			if fields[i] == "dev" {
				runQuiet("ip", "-6", "neigh", "del", "proxy", addr, "dev", fields[i+1])
			}
		}
	}
}

func deleteIP6NATSource(source string) {
	if !commandExists("ip6tables") || strings.TrimSpace(source) == "" {
		return
	}
	for {
		out, err := exec.Command("ip6tables", "-t", "nat", "-S", "POSTROUTING").Output()
		if err != nil {
			return
		}
		deleted := false
		for _, line := range strings.Split(string(out), "\n") {
			if !strings.Contains(line, "-s "+source) || !strings.Contains(line, " -j MASQUERADE") {
				continue
			}
			fields := strings.Fields(line)
			if len(fields) == 0 || fields[0] != "-A" {
				continue
			}
			fields[0] = "-D"
			args := append([]string{"-t", "nat"}, fields...)
			deleted = runCommandOK("ip6tables", args...)
			break
		}
		if !deleted {
			return
		}
	}
}

func removeCLICDNATRules() {
	if commandExists("iptables") {
		for {
			out, err := exec.Command("sh", "-c", "iptables -t nat -L PREROUTING -n --line-numbers 2>/dev/null | grep 'clicd-' | awk '{print $1}' | head -n 1").Output()
			line := strings.TrimSpace(string(out))
			if err != nil || line == "" {
				break
			}
			if !runCommandOK("iptables", "-t", "nat", "-D", "PREROUTING", line) {
				break
			}
		}
		deleteNATRule("POSTROUTING", "-s", config.LXCNATNetwork().Subnet, "-o", "eth+", "-j", "MASQUERADE")
		deleteNATRule("POSTROUTING", "-s", config.KVMNATNetwork().Subnet, "-o", "eth+", "-j", "MASQUERADE")
	}
}

func deleteNATRule(args ...string) {
	fullArgs := append([]string{"-t", "nat", "-D"}, args...)
	for runCommandOK("iptables", fullArgs...) {
	}
}

func deleteFilterRule(args ...string) {
	fullArgs := append([]string{"-D"}, args...)
	for runCommandOK("iptables", fullArgs...) {
	}
}

func deleteIP6FilterRule(args ...string) {
	fullArgs := append([]string{"-D"}, args...)
	for runCommandOK("ip6tables", fullArgs...) {
	}
}

func deleteIP6TablesBridgeRules(bridge string) {
	if !commandExists("ip6tables") {
		return
	}
	for {
		cmd := fmt.Sprintf("ip6tables -S FORWARD 2>/dev/null | grep -- %s | sed 's/^-A /-D /' | head -n 1", shellQuote(bridge))
		out, err := exec.Command("sh", "-c", cmd).Output()
		rule := strings.TrimSpace(string(out))
		if err != nil || rule == "" {
			return
		}
		if !runCommandOK("sh", "-c", "ip6tables "+rule) {
			return
		}
	}
}

func removeCLICDHostHooks() {
	runQuiet("systemctl", "stop", "clicd-kvm-ipv6.service")
	runQuiet("systemctl", "disable", "clicd-kvm-ipv6.service")
	runQuiet("rc-service", "clicd-kvm-ipv6", "stop")
	runQuiet("rc-update", "del", "clicd-kvm-ipv6", "default")
	removePath("/usr/local/sbin/clicd-kvm-ipv6-init")
	removePath("/etc/systemd/system/clicd-kvm-ipv6.service")
	removePath("/etc/local.d/clicd-kvm-ipv6.start")
	removePath("/etc/network/if-up.d/clicd-kvm-ipv6")
}

func removeCLICDQuotaRecords() {
	for _, path := range []string{"/etc/projects", "/etc/projid"} {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var kept []string
		for _, line := range strings.Split(string(data), "\n") {
			if strings.TrimSpace(line) == "" || strings.Contains(line, "clicd-") {
				continue
			}
			kept = append(kept, line)
		}
		_ = os.WriteFile(path, []byte(strings.Join(kept, "\n")+"\n"), 0644)
	}
}

func removeCLICDTmpFiles() {
	for _, pattern := range []string{"/tmp/clicd-*", "/tmp/clicd.*"} {
		matches, _ := filepath.Glob(pattern)
		for _, path := range matches {
			removePath(path)
		}
	}
}

func removeCLICDSwapfile() {
	if !fileExists("/swapfile") {
		return
	}
	runQuiet("swapoff", "/swapfile")
	removePath("/swapfile")
}

func removeLXCContainerPath(path string) {
	unmountPathTree(path)
	detachLoopDevices(path)
	if err := os.RemoveAll(path); err == nil {
		fmt.Printf("Removed %s\n", path)
		return
	}

	runQuiet("fuser", "-km", path+"/rootfs")
	runQuiet("fuser", "-km", path)
	unmountPathTree(path)
	detachLoopDevices(path)
	removePath(path)
}

func unmountPathTree(path string) {
	if commandExists("findmnt") {
		out, err := exec.Command("findmnt", "-R", "-n", "-o", "TARGET", path).Output()
		if err == nil {
			mounts := strings.Split(strings.TrimSpace(string(out)), "\n")
			for i := len(mounts) - 1; i >= 0; i-- {
				mountpoint := strings.TrimSpace(mounts[i])
				if mountpoint != "" {
					runQuiet("umount", "-R", "-l", mountpoint)
					runQuiet("umount", "-l", mountpoint)
				}
			}
		}
	}
	runQuiet("umount", "-R", "-l", path+"/rootfs")
	runQuiet("umount", "-l", path+"/rootfs")
	runQuiet("umount", "-R", "-l", path)
	runQuiet("umount", "-l", path)
}

func detachLoopDevices(path string) {
	if !commandExists("losetup") {
		return
	}
	images := []string{path + "/rootfs.img"}
	if entries, err := os.ReadDir(path); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".img") {
				images = append(images, path+"/"+entry.Name())
			}
		}
	}
	for _, image := range images {
		out, err := exec.Command("losetup", "-j", image).Output()
		if err != nil {
			continue
		}
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if idx := strings.Index(line, ":"); idx > 0 {
				runQuiet("losetup", "-d", line[:idx])
			}
		}
	}
}

func stopAndRemoveService() {
	if commandExists("systemctl") {
		runQuiet("systemctl", "stop", "clicd")
		runQuiet("systemctl", "disable", "clicd")
		removePath("/etc/systemd/system/clicd.service")
		runQuiet("systemctl", "daemon-reload")
		runQuiet("systemctl", "reset-failed", "clicd")
	}

	if commandExists("rc-service") {
		runQuiet("rc-service", "clicd", "stop")
	}
	if commandExists("rc-update") {
		runQuiet("rc-update", "del", "clicd", "default")
	}
	removePath("/etc/init.d/clicd")
}

func removePath(path string) {
	if _, err := os.Lstat(path); os.IsNotExist(err) {
		return
	}
	if err := os.RemoveAll(path); err != nil {
		fmt.Printf("Failed to remove %s: %v\n", path, err)
		return
	}
	fmt.Printf("Removed %s\n", path)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func reloadSysctl() {
	if commandExists("sysctl") {
		runQuiet("sysctl", "--system")
	}
}

func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func runCommandOK(name string, args ...string) bool {
	return exec.Command(name, args...).Run() == nil
}

func runQuiet(name string, args ...string) {
	_ = exec.Command(name, args...).Run()
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func restartWebPanelForConfigChange() {
	if err := restartService("clicd"); err != nil {
		cliPrintf("Web 面板重载跳过: %v\n", err)
		return
	}
	cliPrintln("Web 面板已重载并应用配置变更。")
}

func stopService(name string) error {
	if commandExists("systemctl") {
		return exec.Command("systemctl", "stop", name).Run()
	}
	if commandExists("rc-service") {
		return exec.Command("rc-service", name, "stop").Run()
	}
	return fmt.Errorf("no supported service manager found")
}

func startService(name string) error {
	if commandExists("systemctl") {
		return exec.Command("systemctl", "start", name).Run()
	}
	if commandExists("rc-service") {
		return exec.Command("rc-service", name, "start").Run()
	}
	return fmt.Errorf("no supported service manager found")
}

func restartService(name string) error {
	if commandExists("systemctl") {
		return exec.Command("systemctl", "restart", name).Run()
	}
	if commandExists("rc-service") {
		return exec.Command("rc-service", name, "restart").Run()
	}
	return fmt.Errorf("no supported service manager found")
}

func cliShowInfo() {
	containers, err := manager.ListContainers()
	if err != nil {
		cliPrintf("读取容器状态失败: %v\n", err)
	}

	total := len(containers)
	running := 0
	for _, container := range containers {
		if container.Status == "running" {
			running++
		}
	}

	cliPrintln("\n--- 系统信息 ---")
	cliPrintf("CLICD 版本: %s\n", version.Current())
	cliPrintf("Web 端口: %d\n", config.AppConfig.Port)
	cliPrintf("管理员用户: %s\n", config.AppConfig.AdminUser)
	cliPrintf("容器总数: %d\n", total)
	cliPrintf("运行中: %d\n", running)
	cliPrintf("已停止: %d\n", total-running)

	if hostname, err := os.Hostname(); err == nil {
		cliPrintf("主机名: %s\n", hostname)
	}

	cmd := exec.Command("lxc-info", "--version")
	output, err := cmd.Output()
	if err == nil {
		cliPrintf("LXC 版本: %s", string(output))
	}
}

func selectContainer(reader *bufio.Reader, action string) (int, string) {
	containers, err := manager.ListContainers()
	if err != nil {
		cliPrintf("获取容器列表失败: %v\n", err)
		return 0, ""
	}
	if len(containers) == 0 {
		cliPrintln("暂无可用容器")
		return 0, ""
	}

	cliPrintf("\n--- 选择要%s的容器 ---\n", cliT(action))
	for i, container := range containers {
		fmt.Printf("  %d. [%d] %s [%s]\n", i+1, container.ID, container.Name, container.Status)
	}

	idx := promptInt(reader, "容器", 0)
	if idx < 1 || idx > len(containers) {
		cliPrintln("选择无效")
		return 0, ""
	}

	c := containers[idx-1]
	return c.ID, c.Name
}

func promptString(reader *bufio.Reader, label string, fallback string) string {
	label = cliT(label)
	if fallback == "" {
		fmt.Printf("%s: ", label)
	} else {
		fmt.Printf("%s [%s]: ", label, fallback)
	}

	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)
	if input == "" {
		return fallback
	}
	return input
}

func promptInt(reader *bufio.Reader, label string, fallback int) int {
	input := promptString(reader, label, strconv.Itoa(fallback))
	value, err := strconv.Atoi(input)
	if err != nil || value < 0 {
		return fallback
	}
	return value
}

func promptFloat(reader *bufio.Reader, label string, fallback float64) float64 {
	input := promptString(reader, label, strconv.FormatFloat(fallback, 'f', -1, 64))
	value, err := strconv.ParseFloat(input, 64)
	if err != nil || value < 0 {
		return fallback
	}
	return value
}

func clearScreen() {
	fmt.Print("\033[H\033[2J")
}

func waitEnter(reader *bufio.Reader) {
	cliPrint("\n按 Enter 返回菜单...")
	reader.ReadString('\n')
}

func promptPortList(reader *bufio.Reader, label string) []int {
	input := promptString(reader, label, "")
	if input == "" {
		return nil
	}

	var ports []int
	for _, part := range strings.Split(input, ",") {
		value, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || value <= 0 || value > 65535 {
			cliPrintf("忽略无效端口: %s\n", strings.TrimSpace(part))
			continue
		}
		ports = append(ports, value)
	}
	return ports
}

func detectCLIEnglish() bool {
	lang := strings.ToLower(strings.TrimSpace(os.Getenv("CLICD_LANG")))
	if lang == "en" || strings.HasPrefix(lang, "en_") || strings.HasPrefix(lang, "en-") {
		return true
	}
	if lang == "zh" || strings.HasPrefix(lang, "zh_") || strings.HasPrefix(lang, "zh-") {
		return false
	}
	if config.AppConfig != nil {
		return config.NormalizeLanguage(config.AppConfig.Language) == "en"
	}
	env := strings.ToLower(os.Getenv("LC_ALL") + " " + os.Getenv("LC_MESSAGES") + " " + os.Getenv("LANG"))
	return strings.Contains(env, "en_") || strings.Contains(env, "en-") || strings.Contains(env, "english")
}

func refreshCLILanguage() {
	cliEnglish = detectCLIEnglish()
}

func cliLanguageLabel(language string) string {
	if config.NormalizeLanguage(language) == "en" {
		return "English"
	}
	return cliT("简体中文")
}

func cliT(text string) string {
	if !cliEnglish {
		return text
	}
	translated := text
	keys := make([]string, 0, len(cliTranslations))
	for zh := range cliTranslations {
		keys = append(keys, zh)
	}
	sort.Slice(keys, func(i, j int) bool {
		if len(keys[i]) == len(keys[j]) {
			return keys[i] < keys[j]
		}
		return len(keys[i]) > len(keys[j])
	})
	for _, zh := range keys {
		en := cliTranslations[zh]
		translated = strings.ReplaceAll(translated, zh, en)
	}
	return translated
}

func cliPrint(args ...interface{}) {
	if cliEnglish {
		for i, arg := range args {
			if s, ok := arg.(string); ok {
				args[i] = cliT(s)
			}
		}
	}
	fmt.Print(args...)
}

func cliPrintln(args ...interface{}) {
	if cliEnglish {
		for i, arg := range args {
			if s, ok := arg.(string); ok {
				args[i] = cliT(s)
			}
		}
	}
	fmt.Println(args...)
}

func cliPrintf(format string, args ...interface{}) {
	if cliEnglish {
		for i, arg := range args {
			if s, ok := arg.(string); ok {
				args[i] = cliT(s)
				continue
			}
			if err, ok := arg.(error); ok {
				args[i] = cliT(err.Error())
			}
		}
	}
	fmt.Printf(cliT(format), args...)
}
