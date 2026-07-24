package kvm

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"clicd/internal/config"
	"clicd/internal/lxc"

	"golang.org/x/crypto/ssh"
)

type Manager struct {
	BasePath string
}

const ipv6GatewayLinkLocal = "fe80::1"
const libvirtDefaultNetworkMarker = "/var/lib/clicd/kvm/default-network.created"

type usageSample struct {
	CPUUsec    uint64
	RXBytes    uint64
	TXBytes    uint64
	ReadBytes  uint64
	WriteBytes uint64
	At         time.Time
}

type rateSnapshot struct {
	CPUPct    float64
	RXBps     float64
	TXBps     float64
	ReadBps   float64
	WriteBps  float64
	UpdatedAt time.Time
}

type windowsGuestMetrics struct {
	MemoryUsageBytes int64   `json:"memory_usage_bytes"`
	MemoryTotalBytes int64   `json:"memory_total_bytes"`
	CPULoadPct       float64 `json:"cpu_load_pct"`
	Load1            float64 `json:"load1"`
	Load5            float64 `json:"load5"`
	Load15           float64 `json:"load15"`
}

type windowsGuestMetricsSnapshot struct {
	Metrics   windowsGuestMetrics
	UpdatedAt time.Time
}

type trafficSample struct {
	RXBytes uint64
	TXBytes uint64
}

var (
	usageMu             sync.RWMutex
	lastUsage           = map[string]usageSample{}
	rateCache           = map[string]rateSnapshot{}
	trafficMu           sync.Mutex
	lastTrafficSnapshot = map[string]trafficSample{}
	kvmSnapshotMu       sync.Mutex
	kvmSSHEnsureLocks   sync.Map
	knownSSHHostKeys    sync.Map // TOFU host key store: host:port → ssh.PublicKey
	portMapApplyMu      sync.Mutex
	lastPortMapApply    = map[int]time.Time{}
	windowsMetricsMu    sync.Mutex
	windowsMetricsCache = map[string]windowsGuestMetricsSnapshot{}
	ipv6WarnMu          sync.Mutex
	lastIPv6GuestWarn   = map[int]time.Time{}
)

func BaseDir() string {
	if pool := config.PreferredStoragePoolForContent(config.StorageContentKVM); pool != nil {
		return filepath.Join(pool.Path, "kvm")
	}
	return "/var/lib/clicd/kvm"
}

func NewManager() *Manager {
	return &Manager{BasePath: BaseDir()}
}

func NewManagerForStoragePool(poolID string) *Manager {
	if pool := config.StoragePoolByID(poolID); pool != nil && pool.Enabled {
		for _, content := range pool.ContentTypes {
			if content == config.StorageContentKVM {
				return &Manager{BasePath: filepath.Join(pool.Path, "kvm")}
			}
		}
	}
	return NewManager()
}

func (m *Manager) instancesDir() string {
	return filepath.Join(m.BasePath, "instances")
}

func (m *Manager) instanceDir(name string) string {
	if config.AppConfig != nil {
		for i := range config.AppConfig.Containers {
			c := &config.AppConfig.Containers[i]
			if c.IsKVM() && c.VirshName() == name && strings.TrimSpace(c.DiskImage) != "" {
				return filepath.Dir(c.DiskImage)
			}
		}
	}
	return filepath.Join(m.instancesDir(), name)
}

func ImageDownloadedInfo(id string) (bool, int64) {
	path := ImagePath(id)
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false, 0
	}
	return true, info.Size()
}

// DownloadProgress reports KVM image download/conversion progress.
type DownloadProgress struct {
	Stage           string
	DownloadedBytes int64
	TotalBytes      int64
	Percent         int
}

// DownloadProgressFunc receives download progress updates.
type DownloadProgressFunc func(DownloadProgress)

func DownloadImage(image Image) error {
	return DownloadImageWithProgress(context.Background(), image, nil)
}

func DownloadImageWithProgress(ctx context.Context, image Image, progress DownloadProgressFunc) error {
	pool, err := config.SelectStoragePoolForContent(config.StorageContentImages, "", 1024*1024*1024)
	if err != nil {
		return err
	}
	cacheDir := filepath.Join(pool.Path, "images", "kvm")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return err
	}
	ext := ".qcow2"
	if image.Distro == "windows" {
		ext = ".iso"
	}
	target := filepath.Join(cacheDir, image.ID+ext)
	if ok, _ := ImageDownloadedInfo(image.ID); ok {
		return nil
	}
	// For images with no download URL (e.g. Windows ISO), the user must
	// manually place the file at the expected path.
	if image.URL == "" {
		if _, err := os.Stat(target); err == nil {
			_ = os.Chmod(target, 0644)
			return nil
		}
		return fmt.Errorf("this image has no download URL. Please manually upload the ISO to: %s", target)
	}
	tmp := target + ".tmp"
	_ = os.Remove(tmp)
	if image.Distro == "windows" {
		if err := downloadFileWithValidator(ctx, image.URL, tmp, validateWindowsISOResponse(target), progress); err != nil {
			_ = os.Remove(tmp)
			return err
		}
	} else if err := downloadFile(ctx, image.URL, tmp, progress); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := ctx.Err(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if image.Distro == "windows" {
		if err := validateWindowsISO(tmp, target); err != nil {
			_ = os.Remove(tmp)
			return err
		}
		// Keep Windows ISO as-is, don't convert to qcow2
		if err := os.Rename(tmp, target); err != nil {
			_ = os.Remove(tmp)
			return err
		}
	} else {
		if progress != nil {
			progress(DownloadProgress{Stage: "converting", Percent: 100})
		}
		if err := normalizeQCOW2(ctx, tmp, target); err != nil {
			_ = os.Remove(tmp)
			_ = os.Remove(target)
			return err
		}
	}
	_ = os.Chmod(target, 0644)
	return nil
}

func DeleteImage(id string) error {
	return os.RemoveAll(ImagePath(id))
}

type downloadResponseValidator func(*http.Response) error

func downloadFile(ctx context.Context, url, target string, progress DownloadProgressFunc) error {
	return downloadFileWithValidator(ctx, url, target, nil, progress)
}

func downloadFileWithValidator(ctx context.Context, url, target string, validate downloadResponseValidator, progress DownloadProgressFunc) error {
	client := http.Client{
		Timeout: 30 * time.Minute,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			// Copy User-Agent on redirect
			if ua := via[0].Header.Get("User-Agent"); ua != "" {
				req.Header.Set("User-Agent", ua)
			}
			return nil
		},
	}
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}
	// Windows UA needed for Microsoft download servers
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("download failed: %s", resp.Status)
	}
	if validate != nil {
		if err := validate(resp); err != nil {
			return err
		}
	}
	out, err := os.Create(target)
	if err != nil {
		return err
	}
	defer out.Close()
	total := resp.ContentLength
	if total < 0 {
		total = 0
	}
	if progress != nil {
		progress(DownloadProgress{Stage: "downloading", TotalBytes: total})
	}
	buf := make([]byte, 256*1024)
	var downloaded int64
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			written, writeErr := out.Write(buf[:n])
			downloaded += int64(written)
			if writeErr != nil {
				return writeErr
			}
			if written != n {
				return io.ErrShortWrite
			}
			if progress != nil {
				percent := 0
				if total > 0 {
					percent = int(downloaded * 100 / total)
					if percent > 99 {
						percent = 99
					}
				}
				progress(DownloadProgress{Stage: "downloading", DownloadedBytes: downloaded, TotalBytes: total, Percent: percent})
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return out.Sync()
}

func validateWindowsISOResponse(target string) downloadResponseValidator {
	return func(resp *http.Response) error {
		contentType := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Type")))
		if strings.Contains(contentType, "text/html") || strings.Contains(contentType, "text/plain") {
			return fmt.Errorf("downloaded Windows image response was %q instead of an ISO. Microsoft download links may be region/time limited; manually upload the ISO to: %s", contentType, target)
		}
		finalURL := ""
		if resp.Request != nil && resp.Request.URL != nil {
			finalURL = resp.Request.URL.String()
		}
		path := ""
		if resp.Request != nil && resp.Request.URL != nil {
			path = strings.ToLower(resp.Request.URL.Path)
		}
		looksLikeISO := strings.HasSuffix(path, ".iso") ||
			strings.Contains(contentType, "iso") ||
			strings.Contains(contentType, "octet-stream") ||
			contentType == ""
		if !looksLikeISO {
			return fmt.Errorf("Microsoft redirect did not appear to return an ISO (final URL: %s, Content-Type: %s). Please manually upload the ISO to: %s", finalURL, contentType, target)
		}
		return nil
	}
}

func validateWindowsISO(path, target string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.Size() < 1024*1024*1024 {
		return fmt.Errorf("downloaded Windows ISO is unexpectedly small (%d bytes). Microsoft download links may be region/time limited; try again or manually upload the ISO to: %s", info.Size(), target)
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	header := make([]byte, 512)
	n, err := io.ReadFull(f, header)
	if err != nil && err != io.ErrUnexpectedEOF {
		return err
	}
	prefix := strings.ToLower(strings.TrimSpace(string(header[:n])))
	if strings.HasPrefix(prefix, "<!doctype html") || strings.HasPrefix(prefix, "<html") || strings.Contains(prefix, "<html") {
		return fmt.Errorf("downloaded Windows image is an HTML page instead of an ISO. Microsoft download links may be region/time limited; manually upload the ISO to: %s", target)
	}
	return nil
}

func normalizeQCOW2(ctx context.Context, src, target string) error {
	if err := requireCommand("qemu-img"); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "qemu-img", "convert", "-O", "qcow2", src, target)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("qemu-img convert failed: %v, output: %s", err, string(output))
	}
	return os.Remove(src)
}

func (m *Manager) CreateContainer(cfg lxc.ContainerConfig) error {
	cfg.NormalizeResourceAliases()
	cfg.ReportProgress("preparing", "检查 KVM 镜像与创建参数")
	image := FindImage(cfg.TemplateID)
	if image == nil {
		return fmt.Errorf("KVM image not found: %s", cfg.TemplateID)
	}
	if ok, _ := ImageDownloadedInfo(image.ID); !ok {
		return fmt.Errorf("KVM image is not downloaded: %s", cfg.TemplateID)
	}
	if err := m.validateHost(IsWindowsImage(image.ID)); err != nil {
		return err
	}
	if !config.IsValidContainerName(cfg.Name) {
		return fmt.Errorf("invalid VM name: %s", cfg.Name)
	}
	if config.FindContainerByName(cfg.Name) != nil {
		return fmt.Errorf("container name already exists: %s", cfg.Name)
	}
	if cfg.VCPU < 1 || cfg.VCPU != float64(int(cfg.VCPU)) {
		return fmt.Errorf("KVM vCPU must be a whole number and at least 1")
	}
	if IsWindowsImage(image.ID) && cfg.DiskGB < 30 {
		cfg.DiskGB = 30
	} else if image.Desktop != "" && cfg.DiskGB < 20 {
		cfg.DiskGB = 20
	}
	cfg.ReportProgress("storage", "选择虚拟机存储磁盘")
	pool, err := config.SelectStoragePoolForContent(
		config.StorageContentKVM,
		cfg.StoragePoolID,
		int64(cfg.DiskGB)*1024*1024*1024,
	)
	if err != nil {
		return err
	}
	cfg.StoragePoolID = pool.ID
	m = NewManagerForStoragePool(pool.ID)
	if err := cfg.NormalizeCreateNATMappings(); err != nil {
		return err
	}
	if cfg.SnapshotLimit <= 0 {
		cfg.SnapshotLimit = config.DefaultSnapshotLimit
	}
	if !cfg.ImageLimitConfigured && len(cfg.AllowedImageIDs) == 0 && cfg.TemplateID != "" {
		cfg.AllowedImageIDs = []string{cfg.TemplateID}
		cfg.ImageLimitConfigured = true
	}
	managementPort := 0
	releaseNATReservation := func() {}
	if cfg.WantsNAT() {
		managementPort, releaseNATReservation, err = lxc.ReserveCreateNATPorts(cfg)
		if err != nil {
			return err
		}
		defer releaseNATReservation()
	}

	id := config.AllocateContainerID()
	vmName := fmt.Sprintf("vm-%d", id)
	c, err := m.defineContainer(id, vmName, cfg, true, managementPort)
	if err != nil {
		_ = m.cleanupVM(vmName)
		return err
	}
	config.AddContainer(*c)
	return nil
}

func (m *Manager) defineContainer(id int, vmName string, cfg lxc.ContainerConfig, allocatePorts bool, managementPort int) (*config.Container, error) {
	image := FindImage(cfg.TemplateID)
	if image == nil {
		return nil, fmt.Errorf("KVM image not found: %s", cfg.TemplateID)
	}
	if err := os.MkdirAll(m.instanceDir(vmName), 0700); err != nil {
		return nil, err
	}
	if err := os.Chmod(m.BasePath, 0755); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if err := os.Chmod(m.instancesDir(), 0755); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if err := os.Chmod(m.instanceDir(vmName), 0755); err != nil {
		return nil, err
	}
	diskPath := filepath.Join(m.instanceDir(vmName), "disk.qcow2")
	seedPath := filepath.Join(m.instanceDir(vmName), "seed.iso")
	mac := randomMAC()
	sshPassword := generateRandomString(16)
	sshPublicKey := ""
	sshAuthMode := ""
	if !IsWindowsImage(image.ID) {
		sshAccess, err := lxc.ResolveCreateSSHAccess(cfg)
		if err != nil {
			return nil, err
		}
		sshPassword = sshAccess.Password
		sshPublicKey = sshAccess.PublicKey
		sshAuthMode = sshAccess.Mode
	}
	cfg.ReportProgress("addresses", "分配 IPv4 与 IPv6 地址")
	publicIPv4s, err := lxc.AllocatePublicIPv4Assignments(id, cfg.PublicIPv4s, cfg.IPv4Count, cfg.AssignIPv4)
	if err != nil {
		return nil, err
	}

	ipv6Assignments := []config.IPv6Assignment{}
	if cfg.AssignIPv6 || len(cfg.IPv6Addresses) > 0 {
		assigned, err := m.allocateIPv6AssignmentsForContainer(id, cfg.IPv6Addresses, cfg.IPv6Count, true)
		if err != nil {
			return nil, err
		}
		ipv6Assignments = assigned
	}
	ipv6List := configIPv6AssignmentAddresses(ipv6Assignments)
	ipv4List := configIPv4AssignmentAddresses(publicIPv4s)
	// NAT4 port mappings should bind to the host IP, not the VM's independent public IPv4.
	defaultHostIP := ""

	var xml string
	winAdminPassword := ""
	if IsWindowsImage(image.ID) {
		if cfg.RAMMB < 2048 {
			cfg.RAMMB = 2048
		}
		if cfg.DiskGB < 30 {
			cfg.DiskGB = 30
		}
		cfg.ReportProgress("disk", "创建 Windows 虚拟磁盘")
		if err := createEmptyDisk(diskPath, cfg.DiskGB); err != nil {
			return nil, err
		}
		if err := ensureVirtioWinISO(); err != nil {
			return nil, err
		}
		winAdminPassword = generateWindowsPassword()
		unattendPath := filepath.Join(m.instanceDir(vmName), "unattend.iso")
		cfg.ReportProgress("cloud_init", "生成 Windows 自动应答配置")
		if err := createWindowsUnattendISO(unattendPath, cfg.Name, winAdminPassword, mac, ipv6List, ipv4List); err != nil {
			return nil, err
		}
		xml = windowsDomainXML(vmName, int(cfg.VCPU), cfg.RAMMB, diskPath, ImagePath(image.ID), unattendPath, mac, cfg.IOReadMBps, cfg.IOWriteMBps, cfg.NetworkDownMbps, cfg.NetworkUpMbps)
	} else {
		if image.Desktop != "" {
			if cfg.RAMMB < 2048 {
				cfg.RAMMB = 2048
			}
			if cfg.DiskGB < 20 {
				cfg.DiskGB = 20
			}
		}
		cfg.ReportProgress("disk", "创建 KVM 系统磁盘")
		if err := createOverlayDisk(ImagePath(image.ID), diskPath, cfg.DiskGB); err != nil {
			return nil, err
		}
		cfg.ReportProgress("cloud_init", "生成 cloud-init 初始化配置")
		if err := createSeedISO(seedPath, vmName, cfg.Name, sshPassword, sshPublicKey, mac, ipv6List, ipv4List, *image, sshAuthMode); err != nil {
			return nil, err
		}
		xml = domainXML(vmName, int(cfg.VCPU), cfg.RAMMB, diskPath, seedPath, mac, cfg.IOReadMBps, cfg.IOWriteMBps, cfg.NetworkDownMbps, cfg.NetworkUpMbps, image.Desktop != "")
	}
	xmlPath := filepath.Join(m.instanceDir(vmName), "domain.xml")
	if err := os.WriteFile(xmlPath, []byte(xml), 0644); err != nil {
		return nil, err
	}
	cfg.ReportProgress("define", "注册 KVM 虚拟机")
	cmd := exec.Command("virsh", "define", xmlPath)
	if output, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("virsh define failed: %v, output: %s", err, string(output))
	}

	sshPort := 0
	portMappings := []config.PortMapping{}
	if allocatePorts && cfg.WantsNAT() {
		cfg.ReportProgress("nat", "分配并配置 NAT 端口")
		sshPort = managementPort
		if sshPort <= 0 {
			return nil, fmt.Errorf("NAT management port was not reserved")
		}
		if IsWindowsImage(image.ID) {
			// Windows: RDP (3389) instead of SSH (22)
			portMappings = []config.PortMapping{{
				ContainerPort: 3389,
				HostPort:      sshPort,
				HostIP:        defaultHostIP,
				Protocol:      "tcp",
				Description:   "RDP",
			}}
		} else {
			portMappings = lxc.SetupDefaultPortMappings(sshPort)
			if defaultHostIP != "" {
				for i := range portMappings {
					portMappings[i].HostIP = defaultHostIP
				}
			}
		}
		tempC := &config.Container{ID: id, PublicIPv4s: publicIPv4s, PortMappings: portMappings}
		portMappings, err = lxc.SetupCreatePortMappings(tempC, cfg)
		if err != nil {
			return nil, err
		}
	}

	now := time.Now().Format("2006-01-02 15:04:05")
	trafficMode := cfg.TrafficMode
	if trafficMode == "" {
		trafficMode = "total"
	}
	storagePoolID := cfg.StoragePoolID
	if storagePoolID == "" {
		if pool := config.DefaultStoragePoolForContent(config.StorageContentKVM); pool != nil {
			storagePoolID = pool.ID
		}
	}
	container := &config.Container{
		ID:               id,
		UUID:             config.NewContainerUUID(),
		Name:             cfg.Name,
		Virtualization:   config.VirtualizationKVM,
		KVMName:          vmName,
		DiskImage:        diskPath,
		StoragePoolID:    storagePoolID,
		StoragePath:      m.instanceDir(vmName),
		MACAddress:       mac,
		Template:         cfg.TemplateID,
		VCPU:             cfg.VCPU,
		RAMMB:            cfg.RAMMB,
		DiskGB:           cfg.DiskGB,
		NetworkBWMbps:    cfg.NetworkBWMbps,
		NetworkDownMbps:  cfg.NetworkDownMbps,
		NetworkUpMbps:    cfg.NetworkUpMbps,
		MonthlyTrafficGB: cfg.MonthlyTrafficGB,
		TrafficMode:      trafficMode,
		TrafficInGB:      cfg.TrafficInGB,
		TrafficOutGB:     cfg.TrafficOutGB,
		TrafficResetDate: now[:7],
		IOSpeedMBps:      cfg.IOSpeedMBps,
		IOReadMBps:       cfg.IOReadMBps,
		IOWriteMBps:      cfg.IOWriteMBps,
		PublicIPv4s:      publicIPv4s,
		IPv6Addresses:    ipv6Assignments,
		Status:           "stopped",
		SSHPort:          sshPort,
		SSHPassword: func() string {
			if winAdminPassword != "" {
				return winAdminPassword
			}
			return sshPassword
		}(),
		PortMappings:         portMappings,
		PortMappingLimit:     cfg.PortMappingCount,
		AllowedImageIDs:      append([]string(nil), cfg.AllowedImageIDs...),
		ImageLimitConfigured: cfg.ImageLimitConfigured,
		SnapshotLimit:        config.NormalizeSnapshotLimit(cfg.SnapshotLimit),
		CreatedAt:            now,
		ExpiresAt:            cfg.ExpiresAt,
	}
	container.NormalizeNetworkAssignments()
	cfg.ReportProgress("metadata", "保存虚拟机配置")
	return container, nil
}

func (m *Manager) StartContainer(id int) error {
	c := config.FindContainer(id)
	if c == nil {
		return fmt.Errorf("container not found: %d", id)
	}
	if err := m.validateHost(IsWindowsImage(c.Template)); err != nil {
		return err
	}
	lxc.EnsureAssignedPublicIPv4s(c.PublicIPv4s)
	name := c.VirshName()
	if err := m.ensureDomainDefinition(c); err != nil {
		fmt.Printf("Warning: failed to refresh KVM domain definition for %s: %v\n", name, err)
	}
	status, _ := m.GetContainerStatus(name)
	if status != "running" {
		cmd := exec.Command("virsh", "start", name)
		if output, err := cmd.CombinedOutput(); err != nil {
			if !strings.Contains(strings.ToLower(string(output)), "domain is already active") {
				return fmt.Errorf("virsh start failed: %v, output: %s", err, string(output))
			}
		}
	}
	config.UpdateContainerStatus(id, "initializing")
	// Detect VNC port
	if _, err := m.RefreshVNCPort(id); err != nil {
		fmt.Printf("Warning: failed to refresh VNC port for %s: %v\n", name, err)
	}
	_ = exec.Command("virsh", "dommemstat", name, "--period", "10", "--live").Run()
	_ = exec.Command("virsh", "dommemstat", name, "--period", "10", "--config").Run()
	// Windows VMs need manual install via VNC — don't require IP on first boot
	isWindows := IsWindowsImage(c.Template)
	if isWindows {
		for i := 0; i < 15; i++ {
			if ip, err := m.GetContainerIP(name); err == nil && ip != "" {
				c.IP = ip
				config.SaveConfig()
				break
			}
			time.Sleep(2 * time.Second)
		}
	} else {
		for i := 0; i < 90; i++ {
			if ip, err := m.GetContainerIP(name); err == nil && ip != "" {
				c.IP = ip
				config.SaveConfig()
				break
			}
			time.Sleep(2 * time.Second)
		}
		if c.IP == "" {
			return fmt.Errorf("KVM VM %s started but no IPv4 address was detected", c.Name)
		}
	}
	// Apply port mappings if IP is available (Linux: always; Windows: after installation)
	if c.IP != "" {
		if err := lxc.NewManager().ApplyPortMappings(id); err != nil {
			return err
		}
		if err := lxc.ApplyFirewallRules(id); err != nil {
			fmt.Printf("Warning: failed to apply firewall rules: %v\n", err)
		}
	}
	// Wait for cloud-init to finish and SSH to be reachable (password-only mode)
	if !isWindows && c.IP != "" {
		m.waitForCloudInitReady(name, c.IP, c.SSHPassword)
	}
	config.UpdateContainerStatusAndRestore(id, "running", true)
	if c.IPv6 != "" || len(c.IPv6Addresses) > 0 {
		if err := m.applyIPv6Runtime(c); err != nil {
			return err
		}
	} else {
		ensureKVMIPv6DenyRule("virbr0", c.MACAddress)
	}
	return nil
}

// waitForCloudInitReady waits for cloud-init to finish and SSH to be reachable.
// tofuHostKeyCallback implements Trust-On-First-Use host key verification.
// On the first connection to a host, the key is accepted and remembered.
// Subsequent connections must present the same key or the connection is rejected.
func tofuHostKeyCallback(hostname string, remote net.Addr, key ssh.PublicKey) error {
	if stored, ok := knownSSHHostKeys.Load(hostname); ok {
		if bytes.Equal(stored.(ssh.PublicKey).Marshal(), key.Marshal()) {
			return nil
		}
		return fmt.Errorf("host key mismatch for %s (possible MitM attack)", hostname)
	}
	knownSSHHostKeys.Store(hostname, key)
	return nil
}

func (m *Manager) waitForCloudInitReady(vmName, ip, password string) {
	if ip == "" || password == "" {
		return
	}
	deadline := time.Now().Add(3 * time.Minute)
	target := net.JoinHostPort(ip, "22")
	sshWasUp := false
	qgaAttempted := false

	for time.Now().Before(deadline) {
		client, err := ssh.Dial("tcp", target, &ssh.ClientConfig{
			User:            "root",
			Auth:            []ssh.AuthMethod{ssh.Password(password)},
			HostKeyCallback: tofuHostKeyCallback,
			Timeout:         5 * time.Second,
		})
		if err == nil {
			client.Close()
			if !sshWasUp {
				sshWasUp = true
				fmt.Printf("KVM %s SSH up, waiting for cloud-init to settle...\n", vmName)
				time.Sleep(10 * time.Second)
				continue
			}
			fmt.Printf("KVM %s ready\n", vmName)
			return
		}

		// Try guest agent ONCE to speed things up, with timeout to avoid blocking
		if !qgaAttempted && qemuGuestPing(vmName) == nil {
			qgaAttempted = true
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			cmd := exec.CommandContext(ctx, "virsh", "qemu-agent-command", vmName,
				`{"execute":"guest-exec","arguments":{"path":"/bin/sh","arg":["-c","cloud-init status --wait 2>/dev/null; systemctl restart sshd 2>/dev/null || systemctl restart ssh 2>/dev/null; true"],"capture-output":false}}`)
			cmd.Run()
			cancel()
		}

		time.Sleep(5 * time.Second)
	}
	fmt.Printf("Warning: KVM %s not ready after 3 minutes\n", vmName)
}

func (m *Manager) StopContainer(id int) error {
	c := config.FindContainer(id)
	if c == nil {
		return fmt.Errorf("container not found: %d", id)
	}
	_ = lxc.NewManager().CleanPortMappings(id)
	lxc.CleanFirewallRules(id)
	name := c.VirshName()
	status, _ := m.GetContainerStatus(name)
	if status != "running" {
		config.UpdateContainerStatusAndRestore(id, "stopped", false)
		return nil
	}
	exec.Command("virsh", "shutdown", name).Run()
	for i := 0; i < 20; i++ {
		if status, _ := m.GetContainerStatus(name); status != "running" {
			config.UpdateContainerStatusAndRestore(id, "stopped", false)
			return nil
		}
		time.Sleep(1 * time.Second)
	}
	cmd := exec.Command("virsh", "destroy", name)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("virsh destroy failed: %v, output: %s", err, string(output))
	}
	config.UpdateContainerStatusAndRestore(id, "stopped", false)
	return nil
}

func (m *Manager) RestartContainer(id int) error {
	if err := m.StopContainer(id); err != nil {
		return err
	}
	time.Sleep(1 * time.Second)
	return m.StartContainer(id)
}

func (m *Manager) DestroyContainer(id int) error {
	c := config.FindContainer(id)
	if c == nil {
		return fmt.Errorf("container not found: %d", id)
	}
	name := c.VirshName()
	removeKVMIPv6Runtime(c)
	_ = m.StopContainer(id)
	_ = undefineDomain(name)
	if err := os.RemoveAll(m.instanceDir(name)); err != nil {
		return err
	}
	if !config.RemoveContainer(id) {
		return fmt.Errorf("VM destroyed but config entry was not removed: %d", id)
	}
	return nil
}

func (m *Manager) ReinstallContainer(id int, templateID string, authConfig ...lxc.ContainerConfig) error {
	c := config.FindContainer(id)
	if c == nil {
		return fmt.Errorf("container not found: %d", id)
	}
	image := FindImage(templateID)
	if image == nil {
		return fmt.Errorf("KVM image not found: %s", templateID)
	}
	if ok, _ := ImageDownloadedInfo(image.ID); !ok {
		return fmt.Errorf("KVM image is not downloaded: %s", templateID)
	}
	name := c.VirshName()
	_ = m.StopContainer(id)
	_ = undefineDomain(name)
	_ = os.RemoveAll(m.instanceDir(name))
	cfg := lxc.ContainerConfig{
		Name:             c.Name,
		TemplateID:       templateID,
		VCPU:             c.VCPU,
		RAMMB:            c.RAMMB,
		DiskGB:           c.DiskGB,
		NetworkBWMbps:    c.NetworkBWMbps,
		NetworkDownMbps:  c.NetworkDownMbps,
		NetworkUpMbps:    c.NetworkUpMbps,
		MonthlyTrafficGB: c.MonthlyTrafficGB,
		TrafficMode:      c.TrafficMode,
		TrafficInGB:      c.TrafficInGB,
		TrafficOutGB:     c.TrafficOutGB,
		IOSpeedMBps:      c.IOSpeedMBps,
		IOReadMBps:       c.IOReadMBps,
		IOWriteMBps:      c.IOWriteMBps,
		PortMappingCount: c.PortMappingLimit,
		SnapshotLimit:    c.SnapshotLimit,
		ExpiresAt:        c.ExpiresAt,
	}
	if len(authConfig) > 0 && lxc.HasSSHAuthOptions(authConfig[0]) && !IsWindowsImage(templateID) {
		sshAccess, err := lxc.ResolveReinstallSSHAccess(c.SSHPassword, authConfig[0])
		if err != nil {
			return err
		}
		if sshAccess.PublicKey != "" {
			cfg.SSHAuthMode = lxc.SSHAuthKey
			cfg.SSHPublicKey = sshAccess.PublicKey
		} else {
			cfg.SSHAuthMode = lxc.SSHAuthPassword
		}
		cfg.SSHPassword = sshAccess.Password
	}
	next, err := m.defineContainer(id, name, cfg, false, 0)
	if err != nil {
		return err
	}
	c.Template = templateID
	c.DiskImage = next.DiskImage
	c.MACAddress = next.MACAddress
	c.SSHPassword = next.SSHPassword
	c.SSHHostKey = ""
	c.IP = ""
	c.VNCPort = 0
	normalizeKVMManagementPortMapping(c)
	c.Status = "stopped"
	config.SaveConfig()
	if IsWindowsImage(templateID) {
		return nil
	}
	return m.StartContainer(id)
}

func (m *Manager) ResetSSHPassword(id int, password string) (string, error) {
	c := config.FindContainer(id)
	if c == nil {
		return "", fmt.Errorf("container not found: %d", id)
	}
	if IsWindowsImage(c.Template) {
		return "", fmt.Errorf("Windows KVM administrator password cannot be reset from CLICD yet; change it inside Windows or reinstall to generate a new password")
	}
	if c.Status != "running" {
		return "", fmt.Errorf("KVM VM must be running before password reset")
	}
	if strings.TrimSpace(password) == "" {
		password = generateRandomString(16)
	}
	if err := runKVMGuestAgentSSHSetup(c.VirshName(), password); err == nil {
		c.SSHPassword = password
		c.SSHHostKey = ""
		config.SaveConfig()
		return password, nil
	}
	if c.IP == "" || c.SSHPassword == "" {
		return "", fmt.Errorf("KVM guest agent is not ready and saved SSH credentials are unavailable")
	}
	if err := m.EnsureSSH(id); err != nil {
		return "", err
	}
	chpasswdInput, err := chpasswdStdin("root", password)
	if err != nil {
		return "", err
	}
	client, err := ssh.Dial("tcp", net.JoinHostPort(c.IP, "22"), &ssh.ClientConfig{
		User:            "root",
		Auth:            []ssh.AuthMethod{ssh.Password(c.SSHPassword)},
		HostKeyCallback: kvmHostKeyCallback(c),
		Timeout:         8 * time.Second,
	})
	if err != nil {
		return "", err
	}
	defer client.Close()
	session, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer session.Close()
	session.Stdin = bytes.NewReader(chpasswdInput)
	if output, err := session.CombinedOutput("chpasswd"); err != nil {
		return "", fmt.Errorf("failed to reset password: %v, output: %s", err, string(output))
	}
	c.SSHPassword = password
	c.SSHHostKey = ""
	config.SaveConfig()
	return password, nil
}

func (m *Manager) ApplyContainerLimits(c *config.Container) error {
	if c == nil || !c.IsKVM() {
		return nil
	}
	config.NormalizeContainerResourceAliases(c)
	if c.Status == "running" {
		// Config already saved; domain definition will be refreshed on next start
		return nil
	}
	if c.DiskImage == "" || c.MACAddress == "" {
		return nil
	}
	var xml string
	if IsWindowsImage(c.Template) {
		winISO := ImagePath(c.Template)
		unattendISO := existingWindowsUnattendISO(m.instanceDir(c.VirshName()))
		xml = windowsDomainXML(c.VirshName(), int(c.VCPU), c.RAMMB, c.DiskImage, winISO, unattendISO, c.MACAddress, c.IOReadMBps, c.IOWriteMBps, c.NetworkDownMbps, c.NetworkUpMbps)
	} else {
		seedPath := filepath.Join(m.instanceDir(c.VirshName()), "seed.iso")
		xml = domainXML(c.VirshName(), int(c.VCPU), c.RAMMB, c.DiskImage, seedPath, c.MACAddress, c.IOReadMBps, c.IOWriteMBps, c.NetworkDownMbps, c.NetworkUpMbps, isKVMDesktopTemplate(c.Template))
	}
	xmlPath := filepath.Join(m.instanceDir(c.VirshName()), "domain.xml")
	if err := os.WriteFile(xmlPath, []byte(xml), 0644); err != nil {
		return err
	}
	cmd := exec.Command("virsh", "define", xmlPath)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("virsh define failed: %v, output: %s", err, string(output))
	}
	return nil
}

func (m *Manager) ensureDomainDefinition(c *config.Container) error {
	if c == nil || !c.IsKVM() || c.DiskImage == "" || c.MACAddress == "" {
		return nil
	}
	config.NormalizeContainerResourceAliases(c)
	var xml string
	xmlPath := filepath.Join(m.instanceDir(c.VirshName()), "domain.xml")
	if IsWindowsImage(c.Template) {
		winISO := ImagePath(c.Template)
		unattendISO := existingWindowsUnattendISO(m.instanceDir(c.VirshName()))
		xml = windowsDomainXML(c.VirshName(), int(c.VCPU), c.RAMMB, c.DiskImage, winISO, unattendISO, c.MACAddress, c.IOReadMBps, c.IOWriteMBps, c.NetworkDownMbps, c.NetworkUpMbps)
	} else {
		seedPath := filepath.Join(m.instanceDir(c.VirshName()), "seed.iso")
		xml = domainXML(c.VirshName(), int(c.VCPU), c.RAMMB, c.DiskImage, seedPath, c.MACAddress, c.IOReadMBps, c.IOWriteMBps, c.NetworkDownMbps, c.NetworkUpMbps, isKVMDesktopTemplate(c.Template))
	}
	if err := os.WriteFile(xmlPath, []byte(xml), 0644); err != nil {
		return err
	}
	cmd := exec.Command("virsh", "define", xmlPath)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("virsh define failed: %v, output: %s", err, string(output))
	}
	return nil
}

func (m *Manager) CreateSnapshot(id int, createdBy string, scheduled bool, rotateLimit int, storagePoolID ...string) (config.Snapshot, error) {
	kvmSnapshotMu.Lock()
	defer kvmSnapshotMu.Unlock()

	c := config.FindContainer(id)
	if c == nil {
		return config.Snapshot{}, fmt.Errorf("container not found: %d", id)
	}
	if !c.IsKVM() {
		return config.Snapshot{}, fmt.Errorf("container is not a KVM VM: %d", id)
	}
	if scheduled && rotateLimit > 0 {
		for {
			existing := config.ContainerSnapshots(id)
			if len(existing) < rotateLimit {
				break
			}
			sortSnapshotsOldestFirst(existing)
			if err := m.deleteSnapshotLocked(existing[0]); err != nil {
				return config.Snapshot{}, err
			}
		}
	}

	name := c.VirshName()
	instanceDir := m.instanceDir(name)
	if err := safePathUnder(instanceDir, filepath.Dir(instanceDir)); err != nil {
		return config.Snapshot{}, err
	}
	if _, err := os.Stat(instanceDir); err != nil {
		return config.Snapshot{}, fmt.Errorf("VM storage not found: %v", err)
	}
	pool, err := config.SelectStoragePoolForContent(
		config.StorageContentSnapshots,
		firstString(storagePoolID),
		dirSizeBytes(instanceDir),
	)
	if err != nil {
		return config.Snapshot{}, err
	}

	now := time.Now()
	snapshotID := fmt.Sprintf("snap-%d-%s", id, now.Format("20060102150405-000000000"))
	baseDir := filepath.Join(pool.Path, "snapshots")
	snapshotDir := filepath.Join(baseDir, "kvm", strconv.Itoa(id), snapshotID)
	if err := safePathUnder(snapshotDir, baseDir); err != nil {
		return config.Snapshot{}, err
	}
	if err := os.MkdirAll(snapshotDir, 0700); err != nil {
		return config.Snapshot{}, err
	}

	wasRunning, err := m.prepareVMForColdCopy(id, name)
	if err != nil {
		_ = os.RemoveAll(snapshotDir)
		return config.Snapshot{}, err
	}
	if wasRunning {
		defer func() {
			if err := m.StartContainer(id); err != nil {
				fmt.Printf("Warning: failed to restart %s after snapshot: %v\n", name, err)
			}
		}()
	}

	if err := copyTree(instanceDir, snapshotDir); err != nil {
		_ = os.RemoveAll(snapshotDir)
		return config.Snapshot{}, err
	}

	snapshot := config.Snapshot{
		ID:            snapshotID,
		ContainerID:   c.ID,
		ContainerName: c.Name,
		LXCName:       name,
		CreatedAt:     now.Format("2006-01-02 15:04:05"),
		CreatedBy:     createdBy,
		Scheduled:     scheduled,
		Path:          snapshotDir,
		SizeBytes:     dirSizeBytes(snapshotDir),
	}
	config.AddSnapshot(snapshot)
	return snapshot, nil
}

func (m *Manager) DeleteSnapshot(id string) error {
	kvmSnapshotMu.Lock()
	defer kvmSnapshotMu.Unlock()

	snapshot := config.FindSnapshot(id)
	if snapshot == nil {
		return fmt.Errorf("snapshot not found: %s", id)
	}
	return m.deleteSnapshotLocked(*snapshot)
}

func (m *Manager) deleteSnapshotLocked(snapshot config.Snapshot) error {
	if snapshot.Path != "" {
		if err := safeSnapshotPath(snapshot.Path); err != nil {
			return err
		}
		if err := os.RemoveAll(snapshot.Path); err != nil {
			return fmt.Errorf("failed to delete snapshot files: %v", err)
		}
	}
	config.RemoveSnapshot(snapshot.ID)
	return nil
}

func (m *Manager) RestoreSnapshot(id string) error {
	kvmSnapshotMu.Lock()
	defer kvmSnapshotMu.Unlock()

	snapshot := config.FindSnapshot(id)
	if snapshot == nil {
		return fmt.Errorf("snapshot not found: %s", id)
	}
	if snapshot.Path == "" {
		return fmt.Errorf("snapshot path is empty")
	}
	if err := safeSnapshotPath(snapshot.Path); err != nil {
		return err
	}
	if _, err := os.Stat(snapshot.Path); err != nil {
		return fmt.Errorf("snapshot files not found: %v", err)
	}

	c := config.FindContainer(snapshot.ContainerID)
	if c == nil {
		return fmt.Errorf("container not found: %d", snapshot.ContainerID)
	}
	if !c.IsKVM() {
		return fmt.Errorf("container is not a KVM VM: %d", c.ID)
	}
	name := c.VirshName()
	instanceDir := m.instanceDir(name)
	instanceParent := filepath.Dir(instanceDir)
	if err := safePathUnder(instanceDir, instanceParent); err != nil {
		return err
	}

	wasRunning, err := m.prepareVMForColdCopy(c.ID, name)
	if err != nil {
		return err
	}
	backupDir := filepath.Join(instanceParent, fmt.Sprintf(".%s-restore-backup-%d", name, time.Now().UnixNano()))
	if err := safePathUnder(backupDir, instanceParent); err != nil {
		return err
	}
	if err := os.Rename(instanceDir, backupDir); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to move current VM aside: %v", err)
	}
	if err := copyTree(snapshot.Path, instanceDir); err != nil {
		_ = os.RemoveAll(instanceDir)
		_ = os.Rename(backupDir, instanceDir)
		return fmt.Errorf("failed to restore snapshot: %v", err)
	}
	_ = os.RemoveAll(backupDir)
	if err := undefineDomain(name); err != nil {
		fmt.Printf("Warning: failed to undefine %s before restore redefine: %v\n", name, err)
	}
	xmlPath := filepath.Join(instanceDir, "domain.xml")
	if output, err := exec.Command("virsh", "define", xmlPath).CombinedOutput(); err != nil {
		return fmt.Errorf("virsh define failed after restore: %v, output: %s", err, string(output))
	}
	c.DiskImage = filepath.Join(instanceDir, "disk.qcow2")
	c.StoragePath = instanceDir
	c.Status = "stopped"
	c.IP = ""
	config.SaveConfig()
	if wasRunning {
		return m.StartContainer(c.ID)
	}
	return nil
}

func (m *Manager) SetSnapshotSchedule(id int, enabled bool, intervalHours int, scheduleTime string, createdBy string) (*config.Container, error) {
	c := config.FindContainer(id)
	if c == nil {
		return nil, fmt.Errorf("container not found: %d", id)
	}
	if intervalHours < 24 {
		return nil, fmt.Errorf("snapshot schedule interval cannot be less than 24 hours")
	}
	if _, err := parseScheduleClock(scheduleTime); err != nil {
		return nil, err
	}
	c.SnapshotScheduleEnabled = enabled
	c.SnapshotScheduleIntervalHours = intervalHours
	c.SnapshotScheduleTime = scheduleTime
	c.SnapshotScheduleCreatedBy = createdBy
	if enabled {
		c.SnapshotScheduleNextRun = nextSnapshotRun(time.Now(), intervalHours, scheduleTime).Format(time.RFC3339)
	} else {
		c.SnapshotScheduleNextRun = ""
	}
	if err := config.SaveConfig(); err != nil {
		return nil, err
	}
	return c, nil
}

func (m *Manager) StartSnapshotScheduler() {
	go func() {
		m.runDueSnapshotSchedules()
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			m.runDueSnapshotSchedules()
		}
	}()
}

func (m *Manager) runDueSnapshotSchedules() {
	now := time.Now()
	containers := append([]config.Container(nil), config.AppConfig.Containers...)
	for _, c := range containers {
		if !c.IsKVM() || !c.SnapshotScheduleEnabled {
			continue
		}
		nextRun, err := time.Parse(time.RFC3339, c.SnapshotScheduleNextRun)
		if err != nil || c.SnapshotScheduleNextRun == "" {
			nextRun = now
		}
		if now.Before(nextRun) {
			continue
		}
		createdBy := c.SnapshotScheduleCreatedBy
		if createdBy == "" {
			createdBy = "admin"
		}
		rotateLimit := 0
		if strings.HasPrefix(createdBy, "user:") {
			rotateLimit = config.ContainerSnapshotLimit(&c)
		}
		if _, err := m.CreateSnapshot(c.ID, createdBy, true, rotateLimit); err != nil {
			fmt.Printf("Warning: scheduled KVM snapshot failed for %s: %v\n", c.Name, err)
			continue
		}
		if current := config.FindContainer(c.ID); current != nil {
			interval := current.SnapshotScheduleIntervalHours
			if interval < 24 {
				interval = 24
			}
			next := nextRun.Add(time.Duration(interval) * time.Hour)
			for !next.After(now) {
				next = next.Add(time.Duration(interval) * time.Hour)
			}
			current.SnapshotScheduleLastRun = now.Format(time.RFC3339)
			current.SnapshotScheduleNextRun = next.Format(time.RFC3339)
			config.SaveConfig()
		}
	}
}

func (m *Manager) prepareVMForColdCopy(id int, name string) (bool, error) {
	status, _ := m.GetContainerStatus(name)
	wasRunning := status == "running"
	if wasRunning {
		if err := m.StopContainer(id); err != nil {
			return false, err
		}
		time.Sleep(time.Second)
	} else {
		_ = lxc.NewManager().CleanPortMappings(id)
		lxc.CleanFirewallRules(id)
	}
	return wasRunning, nil
}

func parseScheduleClock(value string) (time.Duration, error) {
	parts := strings.Split(value, ":")
	if len(parts) != 2 {
		return 0, fmt.Errorf("snapshot schedule time must be HH:MM")
	}
	hour, err := strconv.Atoi(parts[0])
	if err != nil || hour < 0 || hour > 23 {
		return 0, fmt.Errorf("snapshot schedule hour must be 00-23")
	}
	minute, err := strconv.Atoi(parts[1])
	if err != nil || minute < 0 || minute > 59 {
		return 0, fmt.Errorf("snapshot schedule minute must be 00-59")
	}
	return time.Duration(hour)*time.Hour + time.Duration(minute)*time.Minute, nil
}

func nextSnapshotRun(from time.Time, intervalHours int, scheduleTime string) time.Time {
	clock, err := parseScheduleClock(scheduleTime)
	if err != nil {
		clock = 3 * time.Hour
	}
	midnight := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, from.Location())
	next := midnight.Add(clock)
	interval := time.Duration(intervalHours) * time.Hour
	for !next.After(from) {
		next = next.Add(interval)
	}
	return next
}

func snapshotBaseDir() string {
	return snapshotBaseDirForPool("")
}

func snapshotBaseDirForPool(poolID string) string {
	if pool, err := config.SelectStoragePoolForContent(config.StorageContentSnapshots, poolID, 0); err == nil {
		return filepath.Join(pool.Path, "snapshots")
	}
	return ""
}

func safeSnapshotPath(path string) error {
	if err := safePathUnder(path, filepath.Join(config.AppConfig.DataDir, "snapshots")); err == nil {
		return nil
	}
	for _, pool := range config.StoragePoolsForContent(config.StorageContentSnapshots) {
		if err := safePathUnder(path, filepath.Join(pool.Path, "snapshots")); err == nil {
			return nil
		}
	}
	return fmt.Errorf("unsafe snapshot path: %s", path)
}

func firstString(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return strings.TrimSpace(values[0])
}

func copyTree(src string, dst string) error {
	if err := os.MkdirAll(dst, 0700); err != nil {
		return err
	}
	output, err := exec.Command("cp", "-a", "--sparse=always", "--reflink=auto", src+string(os.PathSeparator)+".", dst+string(os.PathSeparator)).CombinedOutput()
	if err != nil {
		output, err = exec.Command("cp", "-a", "--sparse=always", src+string(os.PathSeparator)+".", dst+string(os.PathSeparator)).CombinedOutput()
		if err != nil {
			return fmt.Errorf("cp failed: %v, output: %s", err, string(output))
		}
	}
	return nil
}

func dirSizeBytes(path string) int64 {
	out, err := exec.Command("du", "-s", "-B1", path).Output()
	if err != nil {
		return 0
	}
	parts := strings.Fields(string(out))
	if len(parts) == 0 {
		return 0
	}
	var size int64
	fmt.Sscanf(parts[0], "%d", &size)
	return size
}

func safePathUnder(path string, base string) error {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	absBase, err := filepath.Abs(base)
	if err != nil {
		return err
	}
	if absPath == absBase || strings.HasPrefix(absPath, absBase+string(os.PathSeparator)) {
		return nil
	}
	return fmt.Errorf("refusing unsafe path: %s", absPath)
}

func sortSnapshotsOldestFirst(snapshots []config.Snapshot) {
	sort.SliceStable(snapshots, func(i, j int) bool {
		ti, _ := time.Parse("2006-01-02 15:04:05", snapshots[i].CreatedAt)
		tj, _ := time.Parse("2006-01-02 15:04:05", snapshots[j].CreatedAt)
		return ti.Before(tj)
	})
}

func (m *Manager) GetResourceUsage(id int) (map[string]interface{}, error) {
	c := config.FindContainer(id)
	if c == nil {
		return nil, fmt.Errorf("container not found: %d", id)
	}
	name := c.VirshName()
	cpuUsec, rxBytes, txBytes, readBytes, writeBytes := m.getUsageCounters(c)
	usage := map[string]interface{}{
		"memory_usage_bytes": int64(0),
		"memory_total_bytes": int64(0),
		"cpu_usage_usec":     cpuUsec,
		"cpu_usage_pct":      0.0,
		"disk_usage_bytes":   int64(0),
		"network_rx_bytes":   rxBytes,
		"network_tx_bytes":   txBytes,
		"network_rx_bps":     0.0,
		"network_tx_bps":     0.0,
		"disk_read_bytes":    readBytes,
		"disk_write_bytes":   writeBytes,
		"disk_read_bps":      0.0,
		"disk_write_bps":     0.0,
		"load1":              0.0,
		"load5":              0.0,
		"load15":             0.0,
		"guest_metrics":      false,
	}
	if c.DiskImage != "" {
		if info, err := os.Stat(c.DiskImage); err == nil {
			usage["disk_usage_bytes"] = info.Size()
		}
	}
	if c.Status == "running" {
		if mem := virshMemBytes(name); mem > 0 {
			usage["memory_usage_bytes"] = mem
		}
	}
	usageMu.RLock()
	rate, hasRate := rateCache[name]
	usageMu.RUnlock()
	if hasRate && time.Since(rate.UpdatedAt) < 15*time.Second {
		usage["cpu_usage_pct"] = rate.CPUPct
		usage["network_rx_bps"] = rate.RXBps
		usage["network_tx_bps"] = rate.TXBps
		usage["disk_read_bps"] = rate.ReadBps
		usage["disk_write_bps"] = rate.WriteBps
	}
	if c.Status == "running" && IsWindowsImage(c.Template) {
		if metrics, err := m.windowsGuestResourceMetrics(c); err == nil {
			vcpu := c.VCPU
			if vcpu < 1 {
				vcpu = 1
			}
			if metrics.MemoryUsageBytes > 0 {
				usage["memory_usage_bytes"] = metrics.MemoryUsageBytes
			}
			if metrics.MemoryTotalBytes > 0 {
				usage["memory_total_bytes"] = metrics.MemoryTotalBytes
			}
			usage["cpu_usage_pct"] = metrics.CPULoadPct * vcpu
			usage["load1"] = metrics.Load1
			usage["load5"] = metrics.Load5
			usage["load15"] = metrics.Load15
			usage["guest_metrics"] = true
		}
	}
	return usage, nil
}

func (m *Manager) windowsGuestResourceMetrics(c *config.Container) (windowsGuestMetrics, error) {
	if c == nil {
		return windowsGuestMetrics{}, fmt.Errorf("container is nil")
	}
	name := c.VirshName()
	windowsMetricsMu.Lock()
	if cached, ok := windowsMetricsCache[name]; ok && time.Since(cached.UpdatedAt) < 10*time.Second {
		windowsMetricsMu.Unlock()
		return cached.Metrics, nil
	}
	windowsMetricsMu.Unlock()
	if err := qemuGuestPing(name); err != nil {
		return windowsGuestMetrics{}, err
	}
	script := windowsGuestMetricsPowerShell()
	stdout, stderr, err := qemuGuestExecCommandOutput(name, "powershell.exe", []string{"-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", script}, 20*time.Second)
	if err != nil {
		if strings.TrimSpace(stderr) != "" {
			return windowsGuestMetrics{}, fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr))
		}
		return windowsGuestMetrics{}, err
	}
	metrics, err := parseWindowsGuestMetrics(stdout)
	if err != nil {
		return windowsGuestMetrics{}, err
	}
	vcpu := c.VCPU
	if vcpu < 1 {
		vcpu = 1
	}
	loadEquivalent := (metrics.CPULoadPct / 100.0) * vcpu
	metrics.Load1 = loadEquivalent
	metrics.Load5 = loadEquivalent
	metrics.Load15 = loadEquivalent

	windowsMetricsMu.Lock()
	windowsMetricsCache[name] = windowsGuestMetricsSnapshot{Metrics: metrics, UpdatedAt: time.Now()}
	windowsMetricsMu.Unlock()
	return metrics, nil
}

func windowsGuestMetricsPowerShell() string {
	return `$ErrorActionPreference = 'Stop'
$os = Get-CimInstance Win32_OperatingSystem
$cpu = Get-CimInstance Win32_Processor | Measure-Object -Property LoadPercentage -Average
$total = [int64]$os.TotalVisibleMemorySize * 1024
$free = [int64]$os.FreePhysicalMemory * 1024
$used = [Math]::Max([int64]0, $total - $free)
$load = [double]0
if ($null -ne $cpu.Average) { $load = [double]$cpu.Average }
[pscustomobject]@{
  memory_usage_bytes = $used
  memory_total_bytes = $total
  cpu_load_pct = $load
} | ConvertTo-Json -Compress`
}

func parseWindowsGuestMetrics(stdout string) (windowsGuestMetrics, error) {
	text := strings.TrimSpace(stdout)
	start := strings.LastIndex(text, "{")
	end := strings.LastIndex(text, "}")
	if start < 0 || end <= start {
		return windowsGuestMetrics{}, fmt.Errorf("Windows guest metrics returned no JSON: %s", text)
	}
	var metrics windowsGuestMetrics
	if err := json.Unmarshal([]byte(text[start:end+1]), &metrics); err != nil {
		return windowsGuestMetrics{}, fmt.Errorf("invalid Windows guest metrics JSON: %w", err)
	}
	if metrics.CPULoadPct < 0 {
		metrics.CPULoadPct = 0
	}
	if metrics.CPULoadPct > 100 {
		metrics.CPULoadPct = 100
	}
	return metrics, nil
}

func (m *Manager) ListContainers(containers []config.Container) []config.Container {
	for i := range containers {
		if !containers[i].IsKVM() {
			continue
		}
		status, err := m.GetContainerStatus(containers[i].VirshName())
		if err == nil && status != "" {
			containers[i].Status = status
		}
		if status == "running" {
			if _, err := m.RefreshVNCPort(containers[i].ID); err == nil {
				if refreshed := config.FindContainer(containers[i].ID); refreshed != nil {
					containers[i].VNCPort = refreshed.VNCPort
				}
			}
			if ip, err := m.RefreshNetwork(containers[i].ID); err == nil && ip != "" {
				containers[i].IP = ip
			}
		}
	}
	return containers
}

func (m *Manager) RefreshNetwork(id int) (string, error) {
	c := config.FindContainer(id)
	if c == nil {
		return "", fmt.Errorf("container not found: %d", id)
	}
	if !c.IsKVM() {
		return "", fmt.Errorf("container is not a KVM VM: %d", id)
	}
	ip, err := m.GetContainerIP(c.VirshName())
	if err != nil || ip == "" {
		return "", err
	}
	changed := c.IP != ip
	c.IP = ip
	if changed {
		config.SaveConfig()
	}
	if c.Status == "running" && shouldApplyPortMappings(id, changed) {
		if err := lxc.NewManager().ApplyPortMappings(id); err != nil {
			return ip, err
		}
	}
	return ip, nil
}

func shouldApplyPortMappings(id int, force bool) bool {
	portMapApplyMu.Lock()
	defer portMapApplyMu.Unlock()
	now := time.Now()
	if !force {
		if last, ok := lastPortMapApply[id]; ok && now.Sub(last) < time.Minute {
			return false
		}
	}
	lastPortMapApply[id] = now
	return true
}

func (m *Manager) GetContainerStatus(name string) (string, error) {
	cmd := exec.Command("virsh", "domstate", name)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	state := strings.ToLower(strings.TrimSpace(string(out)))
	if strings.Contains(state, "running") {
		return "running", nil
	}
	if strings.Contains(state, "shut") || strings.Contains(state, "off") {
		return "stopped", nil
	}
	return state, nil
}

func (m *Manager) GetContainerIP(name string) (string, error) {
	for _, source := range []string{"lease", "arp", "agent"} {
		cmd := exec.Command("virsh", "domifaddr", name, "--source", source)
		out, err := cmd.Output()
		if err != nil {
			continue
		}
		if ip := firstIPv4(string(out)); ip != "" {
			return ip, nil
		}
	}
	if mac := domainMACAddress(name); mac != "" {
		if ip := dhcpLeaseIP("default", mac); ip != "" {
			return ip, nil
		}
	}
	return "", fmt.Errorf("no IPv4 address found for %s", name)
}

func (m *Manager) validateHost(skipCloudInit bool) error {
	for _, name := range []string{"virsh", "qemu-img"} {
		if err := requireCommand(name); err != nil {
			return err
		}
	}
	if err := requireCommand(kvmEmulatorCommand()); err != nil {
		return err
	}
	if skipCloudInit {
		if runtime.GOARCH != "amd64" {
			return fmt.Errorf("Windows KVM is currently supported only on x86_64/amd64 hosts")
		}
		if err := requireAnyCommand("genisoimage", "mkisofs", "xorriso"); err != nil {
			return fmt.Errorf("%w (needed to generate Windows unattended setup ISO)", err)
		}
	} else {
		if err := requireCommand("cloud-localds"); err != nil {
			return err
		}
	}
	if _, err := os.Stat("/dev/kvm"); err != nil {
		return fmt.Errorf("KVM is not available: /dev/kvm not found")
	}
	if err := ensureDefaultNetwork(); err != nil {
		return err
	}
	return nil
}

func requireCommand(name string) error {
	if _, err := exec.LookPath(name); err != nil {
		return fmt.Errorf("%s is required for KVM support", name)
	}
	return nil
}

func requireAnyCommand(names ...string) error {
	for _, name := range names {
		if _, err := exec.LookPath(name); err == nil {
			return nil
		}
	}
	return fmt.Errorf("one of %s is required for KVM support", strings.Join(names, ", "))
}

func kvmLibvirtArch() string {
	switch runtime.GOARCH {
	case "arm64":
		return "aarch64"
	default:
		return "x86_64"
	}
}

func kvmMachineType() string {
	switch runtime.GOARCH {
	case "arm64":
		return "virt"
	default:
		return "pc"
	}
}

func kvmEmulatorCommand() string {
	switch runtime.GOARCH {
	case "arm64":
		return "qemu-system-aarch64"
	default:
		return "qemu-system-x86_64"
	}
}

func kvmEmulatorPath() string {
	if path, err := exec.LookPath(kvmEmulatorCommand()); err == nil {
		return path
	}
	return "/usr/bin/" + kvmEmulatorCommand()
}

func ensureDefaultNetwork() error {
	// Ensure libvirtd is running
	if err := exec.Command("systemctl", "start", "libvirtd").Run(); err != nil {
		// Non-systemd systems may use a different init, try virsh connect
		if virshCLocaleCommand("connect").Run() != nil {
			return fmt.Errorf("libvirtd is not running and could not be started")
		}
	}
	// Ensure default network is defined
	if virshCLocaleCommand("net-info", "default").Run() != nil {
		// Default network may not be defined; try to define it
		netXML := `<network>
  <name>default</name>
  <bridge name='virbr0'/>
  <forward mode='nat'/>
  <ip address='192.168.122.1' netmask='255.255.255.0'>
    <dhcp>
      <range start='192.168.122.2' end='192.168.122.254'/>
    </dhcp>
  </ip>
</network>`
		tmpFile := filepath.Join(os.TempDir(), "clicd-default-net.xml")
		if err := os.WriteFile(tmpFile, []byte(netXML), 0644); err != nil {
			return fmt.Errorf("failed to write default network XML: %v", err)
		}
		defer os.Remove(tmpFile)
		if out, err := virshCLocaleCommand("net-define", tmpFile).CombinedOutput(); err != nil {
			return fmt.Errorf("failed to define libvirt default network: %v, output: %s", err, string(out))
		}
		if err := os.MkdirAll(filepath.Dir(libvirtDefaultNetworkMarker), 0755); err == nil {
			_ = os.WriteFile(libvirtDefaultNetworkMarker, []byte("created-by-clicd\n"), 0644)
		}
	}
	// Start and autostart the default network
	if out, err := virshCLocaleCommand("net-info", "default").Output(); err == nil {
		if !libvirtNetworkActive(string(out)) {
			if startOut, startErr := virshCLocaleCommand("net-start", "default").CombinedOutput(); startErr != nil {
				if verifyOut, verifyErr := virshCLocaleCommand("net-info", "default").Output(); verifyErr != nil || !libvirtNetworkActive(string(verifyOut)) {
					return fmt.Errorf("failed to start libvirt default network: %v, output: %s", startErr, string(startOut))
				}
			}
		}
	}
	if out, err := virshCLocaleCommand("net-autostart", "default").CombinedOutput(); err != nil {
		return fmt.Errorf("failed to set autostart for libvirt default network: %v, output: %s", err, string(out))
	}
	return nil
}

func virshCLocaleCommand(args ...string) *exec.Cmd {
	cmd := exec.Command("virsh", args...)
	cmd.Env = append(os.Environ(), "LC_ALL=C", "LC_MESSAGES=C", "LANG=C", "LANGUAGE=C")
	return cmd
}

func libvirtNetworkActive(info string) bool {
	for _, line := range strings.Split(info, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(key), "Active") {
			return strings.EqualFold(strings.TrimSpace(value), "yes")
		}
	}
	return false
}

func createOverlayDisk(base, target string, diskGB int) error {
	if diskGB < 1 {
		diskGB = 5
	}
	cmd := exec.Command("qemu-img", "create", "-f", "qcow2", "-F", "qcow2", "-b", base, target)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("qemu-img create failed: %v, output: %s", err, string(output))
	}
	cmd = exec.Command("qemu-img", "resize", target, fmt.Sprintf("%dG", diskGB))
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("qemu-img resize failed: %v, output: %s", err, string(output))
	}
	_ = os.Chmod(target, 0644)
	return nil
}

func ensureVirtioWinISO() error {
	virtioPath := virtioWinISOPath()
	if _, err := os.Stat(virtioPath); err == nil {
		return nil
	}
	if err := os.MkdirAll(CacheDir(), 0755); err != nil {
		return err
	}
	virtioURL := "https://fedorapeople.org/groups/virt/virtio-win/direct-downloads/stable-virtio/virtio-win.iso"
	tmp := virtioPath + ".tmp"
	_ = os.Remove(tmp)
	if err := downloadFile(context.Background(), virtioURL, tmp, nil); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("failed to download virtio-win.iso: %v", err)
	}
	if err := os.Rename(tmp, virtioPath); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	_ = os.Chmod(virtioPath, 0644)
	return nil
}

func createEmptyDisk(target string, diskGB int) error {
	if diskGB < 1 {
		diskGB = 5
	}
	cmd := exec.Command("qemu-img", "create", "-f", "qcow2", target, fmt.Sprintf("%dG", diskGB))
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("qemu-img create empty disk failed: %v, output: %s", err, string(output))
	}
	_ = os.Chmod(target, 0644)
	return nil
}

func createWindowsUnattendISO(target, hostname, adminPassword, mac string, ipv6s []string, ipv4s []string) error {
	tool := firstAvailableCommand("genisoimage", "mkisofs", "xorriso")
	if tool == "" {
		return fmt.Errorf("one of genisoimage, mkisofs, xorriso is required for Windows unattended setup")
	}
	dir := filepath.Join(filepath.Dir(target), "unattend")
	_ = os.RemoveAll(dir)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	defer os.RemoveAll(dir)

	answerPath := filepath.Join(dir, "Autounattend.xml")
	setupScriptsDir := filepath.Join(dir, "$OEM$", "$$", "Setup", "Scripts")
	clicdDir := filepath.Join(dir, "$OEM$", "$1", "CLICD")
	for _, path := range []string{setupScriptsDir, clicdDir} {
		if err := os.MkdirAll(path, 0700); err != nil {
			return err
		}
	}
	if err := os.WriteFile(answerPath, []byte(windowsAutounattendXML(hostname, adminPassword)), 0600); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(setupScriptsDir, "SetupComplete.cmd"), []byte(windowsSetupCompleteCMD()), 0600); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(clicdDir, "FirstLogon.ps1"), []byte(windowsFirstLogonPowerShell(adminPassword, mac, ipv6s, ipv4s)), 0600); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "SetupComplete.cmd"), []byte(windowsSetupCompleteCMD()), 0600); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "FirstLogon.ps1"), []byte(windowsFirstLogonPowerShell(adminPassword, mac, ipv6s, ipv4s)), 0600); err != nil {
		return err
	}
	_ = os.Remove(target)
	var cmd *exec.Cmd
	if tool == "xorriso" {
		cmd = exec.Command(tool, "-as", "mkisofs", "-quiet", "-J", "-r", "-V", "CIDUNATTEND", "-o", target, dir)
	} else {
		cmd = exec.Command(tool, "-quiet", "-J", "-r", "-V", "CIDUNATTEND", "-o", target, dir)
	}
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s failed: %v, output: %s", tool, err, string(output))
	}
	_ = os.Chmod(target, 0644)
	return nil
}

func firstAvailableCommand(names ...string) string {
	for _, name := range names {
		if _, err := exec.LookPath(name); err == nil {
			return name
		}
	}
	return ""
}

func windowsAutounattendXML(hostname, adminPassword string) string {
	if strings.TrimSpace(hostname) == "" {
		hostname = "clicd-win"
	}
	hostname = sanitizeWindowsComputerName(hostname)
	setupCommand := `cmd.exe /c if exist C:\CLICD\FirstLogon.ps1 (powershell.exe -NoProfile -ExecutionPolicy Bypass -File C:\CLICD\FirstLogon.ps1) else (for %%d in (D E F G H I J K L M N O P Q R S T U V W X Y Z) do @if exist %%d:\FirstLogon.ps1 powershell.exe -NoProfile -ExecutionPolicy Bypass -File %%d:\FirstLogon.ps1)`
	return fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<unattend xmlns="urn:schemas-microsoft-com:unattend">
  <settings pass="windowsPE">
    <component name="Microsoft-Windows-International-Core-WinPE" processorArchitecture="amd64" publicKeyToken="31bf3856ad364e35" language="neutral" versionScope="nonSxS" xmlns:wcm="http://schemas.microsoft.com/WMIConfig/2002/State" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
      <SetupUILanguage><UILanguage>en-US</UILanguage></SetupUILanguage>
      <InputLocale>en-US</InputLocale><SystemLocale>en-US</SystemLocale><UILanguage>en-US</UILanguage><UserLocale>en-US</UserLocale>
    </component>
    <component name="Microsoft-Windows-Setup" processorArchitecture="amd64" publicKeyToken="31bf3856ad364e35" language="neutral" versionScope="nonSxS" xmlns:wcm="http://schemas.microsoft.com/WMIConfig/2002/State" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
      <DiskConfiguration>
        <Disk wcm:action="add"><DiskID>0</DiskID><WillWipeDisk>true</WillWipeDisk><CreatePartitions><CreatePartition wcm:action="add"><Order>1</Order><Type>Primary</Type><Size>350</Size></CreatePartition><CreatePartition wcm:action="add"><Order>2</Order><Type>Primary</Type><Extend>true</Extend></CreatePartition></CreatePartitions><ModifyPartitions><ModifyPartition wcm:action="add"><Order>1</Order><PartitionID>1</PartitionID><Label>System</Label><Format>NTFS</Format><Active>true</Active></ModifyPartition><ModifyPartition wcm:action="add"><Order>2</Order><PartitionID>2</PartitionID><Label>Windows</Label><Letter>C</Letter><Format>NTFS</Format></ModifyPartition></ModifyPartitions></Disk>
        <WillShowUI>OnError</WillShowUI>
      </DiskConfiguration>
      <ImageInstall>
        <OSImage>
          <InstallFrom>
            <MetaData wcm:action="add">
              <Key>/IMAGE/INDEX</Key>
              <Value>1</Value>
            </MetaData>
          </InstallFrom>
          <InstallTo><DiskID>0</DiskID><PartitionID>2</PartitionID></InstallTo>
          <WillShowUI>OnError</WillShowUI>
        </OSImage>
      </ImageInstall>
      <UserData>
        <AcceptEula>true</AcceptEula>
        <FullName>CLICD</FullName>
        <Organization>CLICD</Organization>
      </UserData>
    </component>
  </settings>
  <settings pass="specialize">
    <component name="Microsoft-Windows-Shell-Setup" processorArchitecture="amd64" publicKeyToken="31bf3856ad364e35" language="neutral" versionScope="nonSxS" xmlns:wcm="http://schemas.microsoft.com/WMIConfig/2002/State" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
      <ComputerName>%s</ComputerName>
      <TimeZone>UTC</TimeZone>
    </component>
  </settings>
  <settings pass="oobeSystem">
    <component name="Microsoft-Windows-International-Core" processorArchitecture="amd64" publicKeyToken="31bf3856ad364e35" language="neutral" versionScope="nonSxS" xmlns:wcm="http://schemas.microsoft.com/WMIConfig/2002/State" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
      <InputLocale>en-US</InputLocale><SystemLocale>en-US</SystemLocale><UILanguage>en-US</UILanguage><UserLocale>en-US</UserLocale>
    </component>
    <component name="Microsoft-Windows-Shell-Setup" processorArchitecture="amd64" publicKeyToken="31bf3856ad364e35" language="neutral" versionScope="nonSxS" xmlns:wcm="http://schemas.microsoft.com/WMIConfig/2002/State" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
      <AutoLogon><Password><Value>%s</Value><PlainText>true</PlainText></Password><Enabled>true</Enabled><Username>Administrator</Username><LogonCount>1</LogonCount></AutoLogon>
      <UserAccounts><AdministratorPassword><Value>%s</Value><PlainText>true</PlainText></AdministratorPassword></UserAccounts>
      <OOBE><HideEULAPage>true</HideEULAPage><HideLocalAccountScreen>true</HideLocalAccountScreen><HideOEMRegistrationScreen>true</HideOEMRegistrationScreen><HideOnlineAccountScreens>true</HideOnlineAccountScreens><HideWirelessSetupInOOBE>true</HideWirelessSetupInOOBE><ProtectYourPC>3</ProtectYourPC></OOBE>
      <FirstLogonCommands><SynchronousCommand wcm:action="add"><Order>1</Order><Description>CLICD Windows initialization</Description><CommandLine>%s</CommandLine></SynchronousCommand></FirstLogonCommands>
    </component>
  </settings>
</unattend>
`, xmlEscape(hostname), xmlEscape(adminPassword), xmlEscape(adminPassword), xmlEscape(setupCommand))
}

func sanitizeWindowsComputerName(name string) string {
	name = strings.TrimSpace(name)
	var b strings.Builder
	for _, r := range name {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' {
			b.WriteRune(r)
		}
	}
	result := strings.Trim(b.String(), "-")
	if result == "" {
		return "clicd-win"
	}
	if len(result) > 15 {
		result = result[:15]
	}
	return result
}

func windowsSetupCompleteCMD() string {
	return `@echo off
if not exist C:\CLICD mkdir C:\CLICD
if exist C:\CLICD\FirstLogon.ps1 powershell.exe -NoProfile -ExecutionPolicy Bypass -File C:\CLICD\FirstLogon.ps1
exit /b 0
`
}

func windowsFirstLogonPowerShell(adminPassword, mac string, ipv6s []string, ipv4s []string) string {
	commands := []string{
		"$ErrorActionPreference='Continue'",
		"$ProgressPreference='SilentlyContinue'",
		"New-Item -ItemType Directory -Force -Path 'C:\\CLICD' | Out-Null",
		"Start-Transcript -Path 'C:\\CLICD\\init.log' -Append | Out-Null",
		"try {",
		"net user Administrator " + shellQuoteWindows(adminPassword) + " /active:yes",
		"Set-LocalUser -Name 'Administrator' -PasswordNeverExpires $true -ErrorAction SilentlyContinue",
		"Set-ExecutionPolicy -ExecutionPolicy Bypass -Scope LocalMachine -Force",
		windowsAdapterDiscoveryPowerShell(mac),
		"$iface=Wait-ClicdNetworkAdapter",
		"if ($iface) { Enable-NetAdapter -Name $iface.Name -Confirm:$false -ErrorAction SilentlyContinue; Start-Sleep -Seconds 2; $iface=Get-ClicdNetworkAdapter }",
		"if ($iface) { Set-NetIPInterface -InterfaceIndex $iface.ifIndex -AddressFamily IPv4 -Dhcp Enabled -ErrorAction SilentlyContinue }",
		"if ($iface) { Set-DnsClientServerAddress -InterfaceIndex $iface.ifIndex -ResetServerAddresses -ErrorAction SilentlyContinue }",
		"Get-NetConnectionProfile | Set-NetConnectionProfile -NetworkCategory Private -ErrorAction SilentlyContinue",
		"Set-ItemProperty -Path 'HKLM:\\System\\CurrentControlSet\\Control\\Terminal Server' -Name fDenyTSConnections -Value 0",
		"Set-ItemProperty -Path 'HKLM:\\System\\CurrentControlSet\\Control\\Terminal Server\\WinStations\\RDP-Tcp' -Name UserAuthentication -Value 1",
		"Set-Service -Name TermService -StartupType Automatic",
		"Start-Service -Name TermService",
		"Enable-NetFirewallRule -Name 'RemoteDesktop*' -ErrorAction SilentlyContinue",
		"Enable-NetFirewallRule -DisplayGroup 'Remote Desktop' -ErrorAction SilentlyContinue",
		"netsh advfirewall firewall set rule group=\"remote desktop\" new enable=Yes | Out-Null",
		"New-NetFirewallRule -DisplayName 'CLICD RDP TCP 3389' -Direction Inbound -Action Allow -Protocol TCP -LocalPort 3389 -Profile Any -ErrorAction SilentlyContinue | Out-Null",
		"New-NetFirewallRule -DisplayName 'CLICD RDP UDP 3389' -Direction Inbound -Action Allow -Protocol UDP -LocalPort 3389 -Profile Any -ErrorAction SilentlyContinue | Out-Null",
		"$virtio=Get-Volume | Where-Object DriveType -eq 'CD-ROM' | ForEach-Object { $d=$_.DriveLetter; if ($d) { Get-ChildItem ($d+':\\') -Recurse -Filter 'qemu-ga-*.msi' -ErrorAction SilentlyContinue | Select-Object -First 1 } } | Select-Object -First 1",
		"if ($virtio) { Start-Process msiexec.exe -ArgumentList '/i', $virtio.FullName, '/qn' -Wait }",
		"Get-Service QEMU-GA,qemu-ga -ErrorAction SilentlyContinue | Set-Service -StartupType Automatic",
		"Start-Service QEMU-GA,qemu-ga -ErrorAction SilentlyContinue",
	}
	networkCommands := []string{}
	ipv6s = normalizeKVMIPv6List(ipv6s)
	if len(ipv6s) > 0 {
		networkCommands = append(networkCommands, windowsIPv6PowerShell(ipv6s, mac))
	}
	ipv4s = normalizeKVMIPv4List(ipv4s)
	if len(ipv4s) > 0 {
		networkCommands = append(networkCommands, windowsIPv4PowerShell(ipv4s, mac))
	}
	if len(networkCommands) > 0 {
		networkScript := strings.Join(append([]string{
			"$ErrorActionPreference='Continue'",
			"$ProgressPreference='SilentlyContinue'",
			"New-Item -ItemType Directory -Force -Path 'C:\\CLICD' | Out-Null",
		}, networkCommands...), "\r\n") + "\r\n"
		commands = append(commands, windowsPersistentNetworkTaskPowerShell(networkScript))
		commands = append(commands, networkCommands...)
	}
	commands = append(commands,
		"New-Item -ItemType File -Force -Path 'C:\\CLICD\\init.done' | Out-Null",
		"} finally { Stop-Transcript | Out-Null }",
	)
	return strings.Join(commands, "\r\n") + "\r\n"
}

func windowsPersistentNetworkTaskPowerShell(script string) string {
	return strings.Join([]string{
		"$clicdNetworkScript=@'",
		strings.TrimRight(script, "\r\n"),
		"'@",
		"Set-Content -Path 'C:\\CLICD\\ApplyNetwork.ps1' -Value $clicdNetworkScript -Encoding UTF8",
		"$clicdNetworkAction=New-ScheduledTaskAction -Execute 'powershell.exe' -Argument '-NoProfile -ExecutionPolicy Bypass -File C:\\CLICD\\ApplyNetwork.ps1'",
		"$clicdNetworkTrigger=New-ScheduledTaskTrigger -AtStartup",
		"Register-ScheduledTask -TaskName 'CLICD Network Init' -Action $clicdNetworkAction -Trigger $clicdNetworkTrigger -RunLevel Highest -Force -ErrorAction SilentlyContinue | Out-Null",
	}, "\r\n")
}

func windowsAdapterDiscoveryPowerShell(mac string) string {
	targetMAC := strings.ToUpper(strings.NewReplacer(":", "", "-", "", " ", "").Replace(strings.TrimSpace(mac)))
	return strings.Join([]string{
		"$clicdTargetMac=" + powerShellSingleQuote(targetMAC),
		"function Get-ClicdNetworkAdapter {",
		"  $adapters=@(Get-NetAdapter -ErrorAction SilentlyContinue | Where-Object { $_.Status -ne 'Disabled' })",
		"  if ($clicdTargetMac) {",
		"    $matched=$adapters | Where-Object { (($_.MacAddress -replace '[-:]','').ToUpperInvariant()) -eq $clicdTargetMac } | Sort-Object ifIndex | Select-Object -First 1",
		"    if ($matched) { return $matched }",
		"  }",
		"  $up=$adapters | Where-Object { $_.Status -eq 'Up' } | Sort-Object ifIndex | Select-Object -First 1",
		"  if ($up) { return $up }",
		"  return $adapters | Sort-Object ifIndex | Select-Object -First 1",
		"}",
		"function Wait-ClicdNetworkAdapter {",
		"  param([int]$Retries=90,[int]$DelaySeconds=4)",
		"  for ($i=0; $i -lt $Retries; $i++) {",
		"    $adapter=Get-ClicdNetworkAdapter",
		"    if ($adapter) { return $adapter }",
		"    Start-Sleep -Seconds $DelaySeconds",
		"  }",
		"  return $null",
		"}",
	}, "\r\n")
}

func windowsIPv6PowerShell(ipv6s []string, mac string) string {
	ipv6s = normalizeKVMIPv6List(ipv6s)
	if len(ipv6s) == 0 {
		return ""
	}
	quoted := make([]string, 0, len(ipv6s))
	for _, ipv6 := range ipv6s {
		quoted = append(quoted, powerShellSingleQuote(ipv6))
	}
	return strings.Join([]string{
		windowsAdapterDiscoveryPowerShell(mac),
		"if (-not $iface) { $iface=Wait-ClicdNetworkAdapter }",
		"if ($iface) { Enable-NetAdapter -Name $iface.Name -Confirm:$false -ErrorAction SilentlyContinue; Start-Sleep -Seconds 2; $iface=Get-ClicdNetworkAdapter }",
		"$clicdIPv6=@(" + strings.Join(quoted, ",") + ")",
		"if ($iface) {",
		"  foreach ($ip in $clicdIPv6) {",
		"    Get-NetIPAddress -InterfaceIndex $iface.ifIndex -AddressFamily IPv6 -ErrorAction SilentlyContinue | Where-Object { $_.IPAddress -eq $ip } | Remove-NetIPAddress -Confirm:$false -ErrorAction SilentlyContinue",
		"    New-NetIPAddress -IPAddress $ip -PrefixLength 128 -InterfaceIndex $iface.ifIndex -SkipAsSource:$false -ErrorAction SilentlyContinue | Out-Null",
		"  }",
		"  Get-NetRoute -InterfaceIndex $iface.ifIndex -DestinationPrefix '::/0' -ErrorAction SilentlyContinue | Remove-NetRoute -Confirm:$false -ErrorAction SilentlyContinue",
		"  New-NetRoute -DestinationPrefix '::/0' -InterfaceIndex $iface.ifIndex -NextHop '" + ipv6GatewayLinkLocal + "' -RouteMetric 100 -ErrorAction SilentlyContinue | Out-Null",
		"  Set-DnsClientServerAddress -InterfaceIndex $iface.ifIndex -ServerAddresses @('2001:4860:4860::8888','2606:4700:4700::1111') -ErrorAction SilentlyContinue",
		"}",
	}, "\r\n")
}

func windowsIPv4PowerShell(ipv4s []string, mac string) string {
	ipv4s = normalizeKVMIPv4List(ipv4s)
	if len(ipv4s) == 0 {
		return ""
	}
	quoted := make([]string, 0, len(ipv4s))
	for _, ipv4 := range ipv4s {
		quoted = append(quoted, powerShellSingleQuote(ipv4))
	}
	return strings.Join([]string{
		windowsAdapterDiscoveryPowerShell(mac),
		"if (-not $iface) { $iface=Wait-ClicdNetworkAdapter }",
		"if ($iface) { Enable-NetAdapter -Name $iface.Name -Confirm:$false -ErrorAction SilentlyContinue; Start-Sleep -Seconds 2; $iface=Get-ClicdNetworkAdapter }",
		"$clicdIPv4=@(" + strings.Join(quoted, ",") + ")",
		"if ($iface) {",
		"  foreach ($ip in $clicdIPv4) {",
		"    Get-NetIPAddress -InterfaceIndex $iface.ifIndex -AddressFamily IPv4 -ErrorAction SilentlyContinue | Where-Object { $_.IPAddress -eq $ip } | Remove-NetIPAddress -Confirm:$false -ErrorAction SilentlyContinue",
		"    New-NetIPAddress -IPAddress $ip -PrefixLength 32 -InterfaceIndex $iface.ifIndex -SkipAsSource:$false -ErrorAction SilentlyContinue | Out-Null",
		"  }",
		"}",
	}, "\r\n")
}

func normalizeKVMIPv4List(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func powerShellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func shellQuoteWindows(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
}

func createSeedISO(seedPath, instanceID, hostname, password, publicKey, mac string, ipv6s []string, ipv4s []string, image Image, sshAuthMode string) error {
	disablePubkey := sshAuthMode == "password" || sshAuthMode == "auto_password"
	guestSetup := kvmSSHSetupScript(password, disablePubkey, publicKey)
	if desktopSetup := kvmDesktopSetupScript(image); desktopSetup != "" {
		guestSetup += "\n" + desktopSetup
	}
	ipv6s = normalizeKVMIPv6List(ipv6s)
	if len(ipv6s) > 0 {
		guestSetup += "\n" + kvmIPv6SetupScript(ipv6s)
	}
	authorizedKeys := ""
	if publicKey != "" {
		authorizedKeys = fmt.Sprintf(`
    ssh_authorized_keys:
      - %s`, yamlSingleQuote(publicKey))
	}
	setupScript := indentScript(guestSetup, 4)
	userData := fmt.Sprintf(`#cloud-config
preserve_hostname: false
hostname: %s
ssh_pwauth: true
disable_root: false
package_update: true
chpasswd:
  expire: false
  users:
    - name: root
      password: %s
      type: text
users:
  - name: root
    lock_passwd: false%s
runcmd:
  - |
%s
`, hostname, password, authorizedKeys, setupScript)
	metaData := fmt.Sprintf("instance-id: %s\nlocal-hostname: %s\n", instanceID, hostname)

	// Build static address block (IPv4 + IPv6)
	ipv4s = normalizeKVMIPv4List(ipv4s)
	addressBlock := ""
	addressLines := make([]string, 0, len(ipv4s)+len(ipv6s))
	for _, ipv4 := range ipv4s {
		addressLines = append(addressLines, fmt.Sprintf("        - %s/32", ipv4))
	}
	for _, ipv6 := range ipv6s {
		addressLines = append(addressLines, fmt.Sprintf("        - %s/128", ipv6))
	}
	if len(addressLines) > 0 {
		addressBlock = fmt.Sprintf("\n      addresses:\n%s", strings.Join(addressLines, "\n"))
	}

	// IPv6 routes (only needed when IPv6 addresses are configured)
	ipv6RouteBlock := ""
	if len(ipv6s) > 0 {
		ipv6RouteBlock = fmt.Sprintf(`
      routes:
        - to: default
          via: %s
          on-link: true
          metric: 100`, ipv6GatewayLinkLocal)
	}

	networkConfig := fmt.Sprintf(`version: 2
ethernets:
  nic0:
    match:
      macaddress: "%s"
    dhcp4: true
    dhcp6: false%s%s
`, strings.ToLower(mac), addressBlock, ipv6RouteBlock)
	dir := filepath.Dir(seedPath)
	userPath := filepath.Join(dir, "user-data")
	metaPath := filepath.Join(dir, "meta-data")
	networkPath := filepath.Join(dir, "network-config")
	if err := os.WriteFile(userPath, []byte(userData), 0600); err != nil {
		return err
	}
	if err := os.WriteFile(metaPath, []byte(metaData), 0600); err != nil {
		return err
	}
	if err := os.WriteFile(networkPath, []byte(networkConfig), 0600); err != nil {
		return err
	}
	cmd := exec.Command("cloud-localds", "--network-config="+networkPath, seedPath, userPath, metaPath)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("cloud-localds failed: %v, output: %s", err, string(output))
	}
	_ = os.Chmod(seedPath, 0644)
	return nil
}

func configIPv6AssignmentAddresses(assignments []config.IPv6Assignment) []string {
	values := make([]string, 0, len(assignments))
	for _, item := range assignments {
		if strings.TrimSpace(item.Address) != "" {
			values = append(values, strings.TrimSpace(item.Address))
		}
	}
	return values
}

func configIPv4AssignmentAddresses(assignments []config.PublicIPv4Assignment) []string {
	values := make([]string, 0, len(assignments))
	for _, item := range assignments {
		if strings.TrimSpace(item.Address) != "" {
			values = append(values, strings.TrimSpace(item.Address))
		}
	}
	return values
}

func normalizeKVMIPv6List(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func shellQuotedKVMIPv6List(values []string) string {
	values = normalizeKVMIPv6List(values)
	if len(values) == 0 {
		return "''"
	}
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		quoted = append(quoted, shellQuote(value))
	}
	return strings.Join(quoted, " ")
}

func indentScript(script string, spaces int) string {
	prefix := strings.Repeat(" ", spaces)
	lines := strings.Split(strings.TrimRight(script, "\n"), "\n")
	for i, line := range lines {
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n")
}

func yamlSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func isKVMDesktopTemplate(templateID string) bool {
	image := FindImage(templateID)
	return image != nil && image.Desktop != ""
}

func domainXML(name string, vcpu int, ramMB int, diskPath, seedPath, mac string, ioReadMBps int, ioWriteMBps int, networkDownMbps int, networkUpMbps int, desktop bool) string {
	if vcpu < 1 {
		vcpu = 1
	}
	if ramMB < 512 {
		ramMB = 512
	}
	iotune := ""
	if ioReadMBps > 0 || ioWriteMBps > 0 {
		var parts []string
		if ioReadMBps > 0 {
			parts = append(parts, fmt.Sprintf("        <read_bytes_sec>%d</read_bytes_sec>", int64(ioReadMBps)*1024*1024))
		}
		if ioWriteMBps > 0 {
			parts = append(parts, fmt.Sprintf("        <write_bytes_sec>%d</write_bytes_sec>", int64(ioWriteMBps)*1024*1024))
		}
		iotune = fmt.Sprintf(`
      <iotune>
%s
      </iotune>`, strings.Join(parts, "\n"))
	}
	bandwidth := ""
	if networkDownMbps > 0 || networkUpMbps > 0 {
		var parts []string
		if networkDownMbps > 0 {
			parts = append(parts, fmt.Sprintf("        <inbound average='%d'/>", networkDownMbps*128))
		}
		if networkUpMbps > 0 {
			parts = append(parts, fmt.Sprintf("        <outbound average='%d'/>", networkUpMbps*128))
		}
		bandwidth = fmt.Sprintf(`
      <bandwidth>
%s
      </bandwidth>`, strings.Join(parts, "\n"))
	}
	video := "<video><model type='virtio'/></video>"
	input := ""
	if desktop {
		video = "<video><model type='qxl' ram='65536' vram='65536' heads='1' primary='yes'/></video>"
		input = "\n\t    <input type='tablet' bus='usb'/>"
	}
	osAttrs := ""
	features := "<features><acpi/><apic/></features>"
	if runtime.GOARCH == "arm64" {
		osAttrs = " firmware='efi'"
		features = "<features><acpi/><gic version='3'/></features>"
	}
	seedDisk := fmt.Sprintf(`<disk type='file' device='cdrom'>
      <driver name='qemu' type='raw'/>
      <source file='%s'/>
      <target dev='hdb' bus='ide'/>
      <readonly/>
    </disk>`, xmlEscape(seedPath))
	if runtime.GOARCH == "arm64" {
		seedDisk = fmt.Sprintf(`<disk type='file' device='disk'>
      <driver name='qemu' type='raw'/>
      <source file='%s'/>
      <target dev='vdb' bus='virtio'/>
      <readonly/>
    </disk>`, xmlEscape(seedPath))
	}
	return fmt.Sprintf(`<domain type='kvm'>
  <name>%s</name>
  %s
  <memory unit='MiB'>%d</memory>
  <currentMemory unit='MiB'>%d</currentMemory>
  <vcpu placement='static' current='%d'>%d</vcpu>
  <cputune><shares>2048</shares></cputune>
  <os%s>
    <type arch='%s' machine='%s'>hvm</type>
    <boot dev='hd'/>
  </os>
  %s
  <cpu mode='host-passthrough' check='none'/>
  <clock offset='utc'/>
  <on_poweroff>destroy</on_poweroff>
  <on_reboot>restart</on_reboot>
  <on_crash>restart</on_crash>
  <devices>
    <emulator>%s</emulator>
    <disk type='file' device='disk'>
      <driver name='qemu' type='qcow2' cache='none'/>
      <source file='%s'/>
      <target dev='vda' bus='virtio'/>%s
    </disk>
    %s
    <interface type='network'>
      <mac address='%s'/>
      <source network='default'/>
      <model type='virtio'/>%s
    </interface>
    <serial type='pty'><target port='0'/></serial>
    <console type='pty'><target type='serial' port='0'/></console>
    <channel type='unix'>
      <target type='virtio' name='org.qemu.guest_agent.0'/>
    </channel>
    <memballoon model='virtio'>
      <stats period='10'/>
    </memballoon>
    <graphics type='vnc' port='-1' autoport='yes' listen='127.0.0.1'/>%s
    %s
  </devices>
</domain>`, xmlEscape(name), domainUUIDXML(name), ramMB, ramMB, vcpu, vcpu, osAttrs, kvmLibvirtArch(), kvmMachineType(), features, xmlEscape(kvmEmulatorPath()), xmlEscape(diskPath), iotune, seedDisk, xmlEscape(mac), bandwidth, input, video)
}

func windowsDomainXML(name string, vcpu int, ramMB int, diskPath, winISOPath, unattendISOPath, mac string, ioReadMBps int, ioWriteMBps int, networkDownMbps int, networkUpMbps int) string {
	if vcpu < 1 {
		vcpu = 1
	}
	if ramMB < 2048 {
		ramMB = 2048
	}
	iotune := ""
	if ioReadMBps > 0 || ioWriteMBps > 0 {
		var parts []string
		if ioReadMBps > 0 {
			parts = append(parts, fmt.Sprintf("        <read_bytes_sec>%d</read_bytes_sec>", int64(ioReadMBps)*1024*1024))
		}
		if ioWriteMBps > 0 {
			parts = append(parts, fmt.Sprintf("        <write_bytes_sec>%d</write_bytes_sec>", int64(ioWriteMBps)*1024*1024))
		}
		iotune = fmt.Sprintf(`
      <iotune>
%s
      </iotune>`, strings.Join(parts, "\n"))
	}
	bandwidth := ""
	if networkDownMbps > 0 || networkUpMbps > 0 {
		var parts []string
		if networkDownMbps > 0 {
			parts = append(parts, fmt.Sprintf("        <inbound average='%d'/>", networkDownMbps*128))
		}
		if networkUpMbps > 0 {
			parts = append(parts, fmt.Sprintf("        <outbound average='%d'/>", networkUpMbps*128))
		}
		bandwidth = fmt.Sprintf(`
      <bandwidth>
%s
      </bandwidth>`, strings.Join(parts, "\n"))
	}
	virtioWinISO := virtioWinISOPath()
	unattendDisk := ""
	if strings.TrimSpace(unattendISOPath) != "" {
		unattendDisk = fmt.Sprintf(`
    <disk type='file' device='cdrom'>
      <driver name='qemu' type='raw'/>
      <source file='%s'/>
      <target dev='hdd' bus='ide'/>
      <readonly/>
    </disk>`, xmlEscape(unattendISOPath))
	}
	return fmt.Sprintf(`<domain type='kvm'>
  <name>%s</name>
  %s
  <memory unit='MiB'>%d</memory>
  <currentMemory unit='MiB'>%d</currentMemory>
  <vcpu placement='static' current='%d'>%d</vcpu>
  <cputune><shares>2048</shares></cputune>
  <os>
    <type arch='x86_64' machine='pc'>hvm</type>
  </os>
  <features>
    <acpi/>
    <apic/>
    <hyperv mode='custom'>
      <relaxed state='on'/>
      <vapic state='on'/>
      <spinlocks state='on' retries='8191'/>
    </hyperv>
  </features>
  <cpu mode='host-passthrough' check='none'>
    <topology sockets='1' cores='%d' threads='1'/>
  </cpu>
  <clock offset='localtime'>
    <timer name='hypervclock' present='yes'/>
  </clock>
  <on_poweroff>destroy</on_poweroff>
  <on_reboot>restart</on_reboot>
  <on_crash>restart</on_crash>
  <devices>
    <emulator>/usr/bin/qemu-system-x86_64</emulator>
    <disk type='file' device='disk'>
      <driver name='qemu' type='qcow2' cache='none'/>
      <source file='%s'/>
      <target dev='sda' bus='sata'/>
      <boot order='2'/>%s
    </disk>
    <disk type='file' device='cdrom'>
      <driver name='qemu' type='raw'/>
      <source file='%s'/>
      <target dev='hdb' bus='ide'/>
      <readonly/>
      <boot order='1'/>
    </disk>
    <disk type='file' device='cdrom'>
      <driver name='qemu' type='raw'/>
      <source file='%s'/>
      <target dev='hdc' bus='ide'/>
      <readonly/>
    </disk>%s
    <interface type='network'>
      <mac address='%s'/>
      <source network='default'/>
      <model type='e1000e'/>%s
    </interface>
    <channel type='unix'>
      <target type='virtio' name='org.qemu.guest_agent.0'/>
    </channel>
    <input type='tablet' bus='usb'/>
    <graphics type='vnc' port='-1' autoport='yes' listen='127.0.0.1'/>
    <video><model type='qxl'/></video>
  </devices>
</domain>`, xmlEscape(name), domainUUIDXML(name), ramMB, ramMB, vcpu, vcpu, vcpu,
		xmlEscape(diskPath), iotune,
		xmlEscape(winISOPath), xmlEscape(virtioWinISO), unattendDisk, xmlEscape(mac), bandwidth)
}

func xmlEscape(value string) string {
	return html.EscapeString(value)
}

func existingWindowsUnattendISO(instanceDir string) string {
	path := filepath.Join(instanceDir, "unattend.iso")
	if _, err := os.Stat(path); err == nil {
		return path
	}
	return ""
}

func domainUUIDXML(name string) string {
	out, err := exec.Command("virsh", "domuuid", name).Output()
	if err != nil {
		return ""
	}
	uuid := strings.TrimSpace(string(out))
	if uuid == "" {
		return ""
	}
	return fmt.Sprintf("<uuid>%s</uuid>", xmlEscape(uuid))
}

func (m *Manager) cleanupVM(name string) error {
	_ = exec.Command("virsh", "destroy", name).Run()
	_ = undefineDomain(name)
	return os.RemoveAll(m.instanceDir(name))
}

func undefineDomain(name string) error {
	if err := exec.Command("virsh", "undefine", name, "--nvram").Run(); err == nil {
		return nil
	}
	return exec.Command("virsh", "undefine", name).Run()
}

func (m *Manager) RefreshVNCPort(id int) (int, error) {
	c := config.FindContainer(id)
	if c == nil {
		return 0, fmt.Errorf("container not found: %d", id)
	}
	if !c.IsKVM() {
		return 0, fmt.Errorf("container is not a KVM VM: %d", id)
	}
	port := getVNCPort(c.VirshName())
	if port <= 0 {
		return 0, fmt.Errorf("VNC display is not available for %s", c.VirshName())
	}
	if c.VNCPort != port {
		c.VNCPort = port
		config.SaveConfig()
	}
	return port, nil
}

func normalizeKVMManagementPortMapping(c *config.Container) {
	if c == nil || !c.IsKVM() {
		return
	}
	hostPort := c.SSHPort
	if hostPort <= 0 {
		allocated, err := config.AllocateSSHPort()
		if err != nil {
			return
		}
		hostPort = allocated
		c.SSHPort = hostPort
	}
	desiredPort := 22
	description := "SSH"
	if IsWindowsImage(c.Template) {
		desiredPort = 3389
		description = "RDP"
	}
	mapping := config.PortMapping{
		ContainerPort: desiredPort,
		HostPort:      hostPort,
		Protocol:      "tcp",
		Description:   description,
	}
	for i, pm := range c.PortMappings {
		if strings.EqualFold(pm.Description, "SSH") || strings.EqualFold(pm.Description, "RDP") || pm.ContainerPort == 22 || pm.ContainerPort == 3389 || pm.HostPort == hostPort {
			if pm.HostPort > 0 {
				mapping.HostPort = pm.HostPort
				c.SSHPort = pm.HostPort
			}
			c.PortMappings[i] = mapping
			return
		}
	}
	c.PortMappings = append([]config.PortMapping{mapping}, c.PortMappings...)
}

func getVNCPort(name string) int {
	out, err := exec.Command("virsh", "domdisplay", name).Output()
	if err != nil {
		return 0
	}
	display := strings.TrimSpace(string(out))
	// virsh domdisplay returns "vnc://127.0.0.1:0" or "vnc://127.0.0.1:5901"
	if idx := strings.LastIndex(display, ":"); idx >= 0 {
		portStr := display[idx+1:]
		port, err := strconv.Atoi(portStr)
		if err != nil {
			return 0
		}
		// Port 0 means VNC display 0 → actual port 5900
		if port < 5900 {
			port += 5900
		}
		return port
	}
	return 0
}

func firstIPv4(output string) string {
	re := regexp.MustCompile(`\b((?:\d{1,3}\.){3}\d{1,3})(?:/\d+)?\b`)
	for _, match := range re.FindAllStringSubmatch(output, -1) {
		if len(match) > 1 && net.ParseIP(match[1]) != nil && !strings.HasPrefix(match[1], "127.") {
			return match[1]
		}
	}
	return ""
}

func domainMACAddress(name string) string {
	out, err := exec.Command("virsh", "domiflist", name).Output()
	if err != nil {
		return ""
	}
	macRE := regexp.MustCompile(`(?i)\b[0-9a-f]{2}(?::[0-9a-f]{2}){5}\b`)
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(strings.ToLower(line), "network") || strings.Contains(strings.ToLower(line), "default") {
			if mac := macRE.FindString(line); mac != "" {
				return strings.ToLower(mac)
			}
		}
	}
	if mac := macRE.FindString(string(out)); mac != "" {
		return strings.ToLower(mac)
	}
	return ""
}

func dhcpLeaseIP(networkName string, mac string) string {
	if mac == "" {
		return ""
	}
	out, err := exec.Command("virsh", "net-dhcp-leases", networkName, "--mac", mac).Output()
	if err != nil {
		return ""
	}
	return firstIPv4(string(out))
}

func kvmSSHEnsureLock(id int) *sync.Mutex {
	lock, _ := kvmSSHEnsureLocks.LoadOrStore(id, &sync.Mutex{})
	return lock.(*sync.Mutex)
}

// EnsureSSH verifies root password SSH, installs missing SSH/agent packages, and
// refreshes the SSH config for cloud images whose first boot is still settling.
func (m *Manager) EnsureSSH(id int) error {
	lock := kvmSSHEnsureLock(id)
	lock.Lock()
	defer lock.Unlock()

	c := config.FindContainer(id)
	if c == nil {
		return fmt.Errorf("container not found: %d", id)
	}
	if !c.IsKVM() {
		return fmt.Errorf("container is not a KVM VM: %d", id)
	}
	// Windows VMs are managed via VNC, not SSH
	if IsWindowsImage(c.Template) {
		return nil
	}
	status, _ := m.GetContainerStatus(c.VirshName())
	if status != "running" {
		return fmt.Errorf("KVM VM %s is not running; cannot configure SSH", c.Name)
	}
	if err := m.ensureDomainDefinition(c); err != nil {
		fmt.Printf("Warning: failed to refresh KVM domain definition for %s: %v\n", c.VirshName(), err)
	}
	if c.SSHPassword == "" {
		return fmt.Errorf("KVM SSH password is empty; reinstall or reset password after boot")
	}

	deadline := time.Now().Add(4 * time.Minute)
	var lastErr error
	qgaAttempted := false
	for time.Now().Before(deadline) {
		if c.IP == "" {
			if ip, err := m.GetContainerIP(c.VirshName()); err == nil && ip != "" {
				c.IP = ip
				config.SaveConfig()
			}
		}
		if c.IP == "" {
			lastErr = fmt.Errorf("waiting for VM IPv4 address")
			time.Sleep(3 * time.Second)
			continue
		}

		client, err := ssh.Dial("tcp", net.JoinHostPort(c.IP, "22"), &ssh.ClientConfig{
			User:            "root",
			Auth:            []ssh.AuthMethod{ssh.Password(c.SSHPassword)},
			HostKeyCallback: kvmHostKeyCallback(c),
			Timeout:         8 * time.Second,
		})
		if err != nil {
			lastErr = err
			if !qgaAttempted {
				qgaAttempted = true
				if setupErr := runKVMGuestAgentSSHSetup(c.VirshName(), c.SSHPassword); setupErr != nil {
					lastErr = fmt.Errorf("%v; guest-agent fallback failed: %w", err, setupErr)
					if strings.Contains(setupErr.Error(), "QEMU guest agent is not active") {
						return fmt.Errorf("KVM SSH is not reachable for %s, and QEMU guest agent is not active. Restart this VM once to attach the guest-agent channel, then try WebSSH again. If it was created before KVM SSH initialization support and still fails after restart, reinstall it", c.Name)
					}
				}
			}
			time.Sleep(5 * time.Second)
			continue
		}
		err = runKVMSSHSetup(client, c.SSHPassword)
		_ = client.Close()
		if err != nil {
			lastErr = err
			time.Sleep(5 * time.Second)
			continue
		}
		if mapErr := lxc.NewManager().ApplyPortMappings(id); mapErr != nil {
			return mapErr
		}
		if err := lxc.ApplyFirewallRules(id); err != nil {
			fmt.Printf("Warning: failed to apply firewall rules: %v\n", err)
		}
		return nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("timed out waiting for SSH")
	}
	return fmt.Errorf("KVM SSH initialization failed for %s: %v", c.Name, lastErr)
}

func runKVMGuestAgentSSHSetup(name string, password string) error {
	if err := qemuGuestPing(name); err != nil {
		return err
	}
	return qemuGuestExec(name, kvmSSHSetupScript(password, false), 180*time.Second)
}

func runKVMSSHSetup(client *ssh.Client, password string) error {
	return runKVMSSHScript(client, kvmSSHSetupScript(password, false), "KVM SSH", 150*time.Second)
}

func runKVMSSHScript(client *ssh.Client, script string, description string, timeout time.Duration) error {
	session, err := client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()
	done := make(chan error, 1)
	var output []byte
	go func() {
		var err error
		session.Stdin = strings.NewReader(script)
		output, err = session.CombinedOutput("sh -s")
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("failed to configure %s: %v, output: %s", description, err, string(output))
		}
		return nil
	case <-time.After(timeout):
		_ = session.Close()
		return fmt.Errorf("timed out configuring %s after %s", description, timeout)
	}
}

func kvmSSHSetupScript(password string, disablePubkeyAuth bool, publicKeys ...string) string {
	publicKey := ""
	if len(publicKeys) > 0 {
		publicKey = strings.TrimSpace(publicKeys[0])
	}
	pubkeyValue := "yes"
	if disablePubkeyAuth {
		pubkeyValue = "no"
	}
	script := `set -u
ROOT_PASSWORD=` + shellQuote(password) + `
SSH_PUBLIC_KEY=` + shellQuote(publicKey) + `
export DEBIAN_FRONTEND=noninteractive
if command -v apt-get >/dev/null 2>&1; then
	if ! command -v sshd >/dev/null 2>&1 || ! command -v qemu-ga >/dev/null 2>&1; then
		apt-get update || true
		apt-get install -y openssh-server qemu-guest-agent || true
	fi
fi
if command -v dnf >/dev/null 2>&1; then
	if ! command -v sshd >/dev/null 2>&1 || ! command -v qemu-ga >/dev/null 2>&1; then
		dnf install -y openssh-server qemu-guest-agent || true
	fi
fi
if command -v yum >/dev/null 2>&1; then
	if ! command -v sshd >/dev/null 2>&1 || ! command -v qemu-ga >/dev/null 2>&1; then
		yum install -y openssh-server qemu-guest-agent || true
	fi
fi
if command -v apk >/dev/null 2>&1; then
	if ! command -v sshd >/dev/null 2>&1 || ! command -v qemu-ga >/dev/null 2>&1; then
		apk update || true
		apk add --no-cache openssh qemu-guest-agent shadow iproute2 || true
	fi
fi
mkdir -p /etc/ssh/sshd_config.d
cat > /etc/ssh/sshd_config.d/99-clicd-root.conf <<'EOF'
PermitRootLogin yes
PubkeyAuthentication __CLICD_PUBKEY_AUTH__
PasswordAuthentication yes
KbdInteractiveAuthentication yes
ChallengeResponseAuthentication yes
EOF
if [ -f /etc/ssh/sshd_config ]; then
	grep -q '^PermitRootLogin ' /etc/ssh/sshd_config && sed -i 's/^PermitRootLogin .*/PermitRootLogin yes/' /etc/ssh/sshd_config || printf '\nPermitRootLogin yes\n' >> /etc/ssh/sshd_config
	grep -q '^#PermitRootLogin ' /etc/ssh/sshd_config && sed -i 's/^#PermitRootLogin .*/PermitRootLogin yes/' /etc/ssh/sshd_config || true
	grep -q '^PubkeyAuthentication ' /etc/ssh/sshd_config && sed -i 's/^PubkeyAuthentication .*/PubkeyAuthentication __CLICD_PUBKEY_AUTH__/' /etc/ssh/sshd_config || printf '\nPubkeyAuthentication __CLICD_PUBKEY_AUTH__\n' >> /etc/ssh/sshd_config
	grep -q '^#PubkeyAuthentication ' /etc/ssh/sshd_config && sed -i 's/^#PubkeyAuthentication .*/PubkeyAuthentication __CLICD_PUBKEY_AUTH__/' /etc/ssh/sshd_config || true
	grep -q '^PasswordAuthentication ' /etc/ssh/sshd_config && sed -i 's/^PasswordAuthentication .*/PasswordAuthentication yes/' /etc/ssh/sshd_config || printf '\nPasswordAuthentication yes\n' >> /etc/ssh/sshd_config
	grep -q '^#PasswordAuthentication ' /etc/ssh/sshd_config && sed -i 's/^#PasswordAuthentication .*/PasswordAuthentication yes/' /etc/ssh/sshd_config || true
	grep -q '^KbdInteractiveAuthentication ' /etc/ssh/sshd_config && sed -i 's/^KbdInteractiveAuthentication .*/KbdInteractiveAuthentication yes/' /etc/ssh/sshd_config || printf '\nKbdInteractiveAuthentication yes\n' >> /etc/ssh/sshd_config
	grep -q '^#KbdInteractiveAuthentication ' /etc/ssh/sshd_config && sed -i 's/^#KbdInteractiveAuthentication .*/KbdInteractiveAuthentication yes/' /etc/ssh/sshd_config || true
fi
if [ -n "$SSH_PUBLIC_KEY" ]; then
	mkdir -p /root/.ssh
	touch /root/.ssh/authorized_keys
	grep -qxF "$SSH_PUBLIC_KEY" /root/.ssh/authorized_keys 2>/dev/null || printf '%s\n' "$SSH_PUBLIC_KEY" >> /root/.ssh/authorized_keys
	chmod 700 /root/.ssh
	chmod 600 /root/.ssh/authorized_keys
	chown -R root:root /root/.ssh 2>/dev/null || true
fi
if command -v chpasswd >/dev/null 2>&1; then
	printf 'root:%s\n' "$ROOT_PASSWORD" | chpasswd 2>/tmp/clicd-chpasswd.log && echo "root password set via chpasswd" || echo "WARNING: chpasswd failed: $(cat /tmp/clicd-chpasswd.log 2>/dev/null)"
elif command -v openssl >/dev/null 2>&1 && command -v usermod >/dev/null 2>&1; then
	HASH=$(echo "$ROOT_PASSWORD" | openssl passwd -6 -stdin 2>/dev/null)
	[ -n "$HASH" ] && usermod -p "$HASH" root 2>/dev/null && echo "root password set via openssl/usermod" || echo "WARNING: openssl/usermod failed"
fi
ssh-keygen -A >/dev/null 2>&1 || true
if command -v systemctl >/dev/null 2>&1; then
	systemctl enable --now qemu-guest-agent >/dev/null 2>&1 || true
	systemctl enable --now getty@tty1.service >/dev/null 2>&1 || true
	systemctl restart getty@tty1.service >/dev/null 2>&1 || true
	systemctl restart ssh >/dev/null 2>&1 || systemctl restart sshd >/dev/null 2>&1 || systemctl enable --now ssh >/dev/null 2>&1 || systemctl enable --now sshd >/dev/null 2>&1 || true
fi
if command -v rc-update >/dev/null 2>&1; then
	rc-update add sshd default >/dev/null 2>&1 || true
	rc-update add qemu-guest-agent default >/dev/null 2>&1 || rc-update add qemu-ga default >/dev/null 2>&1 || true
	rc-update add agetty.tty1 default >/dev/null 2>&1 || true
	rc-service qemu-guest-agent start >/dev/null 2>&1 || rc-service qemu-ga start >/dev/null 2>&1 || true
	rc-service agetty.tty1 restart >/dev/null 2>&1 || true
	rc-service sshd restart >/dev/null 2>&1 || /etc/init.d/sshd restart >/dev/null 2>&1 || true
fi
service qemu-guest-agent start >/dev/null 2>&1 || service qemu-ga start >/dev/null 2>&1 || true
service ssh restart >/dev/null 2>&1 || service sshd restart >/dev/null 2>&1 || true
if command -v chvt >/dev/null 2>&1; then
	chvt 1 >/dev/null 2>&1 || true
fi
if [ -w /dev/tty1 ]; then
	printf '\nCLICD VNC console is ready. Press Enter for login prompt.\n' >/dev/tty1 || true
fi
`
	script = strings.ReplaceAll(script, "__CLICD_PUBKEY_AUTH__", pubkeyValue)
	return script
}

func kvmDesktopSetupScript(image Image) string {
	if strings.ToLower(strings.TrimSpace(image.Desktop)) != "xfce" {
		return ""
	}
	packages := ""
	switch image.Distro {
	case "ubuntu":
		packages = "xubuntu-desktop"
	case "debian":
		packages = "task-xfce-desktop"
	default:
		return ""
	}
	return `if command -v apt-get >/dev/null 2>&1; then
	{
		exec >>/var/log/clicd-desktop-setup.log 2>&1
		echo "CLICD XFCE setup started at $(date -Is)"
		export DEBIAN_FRONTEND=noninteractive
		export APT_LISTCHANGES_FRONTEND=none
		apt-get update || true
		apt-get install -y -o Dpkg::Options::=--force-confdef -o Dpkg::Options::=--force-confold ` + packages + ` || apt-get install -y -o Dpkg::Options::=--force-confdef -o Dpkg::Options::=--force-confold xfce4 lightdm lightdm-gtk-greeter dbus-x11 xorg || true
		if command -v useradd >/dev/null 2>&1 && ! id clicd >/dev/null 2>&1; then
			useradd -m -s /bin/bash clicd || true
		fi
		if command -v chpasswd >/dev/null 2>&1 && id clicd >/dev/null 2>&1; then
			printf 'clicd:%s\n' "$ROOT_PASSWORD" | chpasswd || true
		fi
		usermod -aG sudo clicd >/dev/null 2>&1 || true
		usermod -aG autologin clicd >/dev/null 2>&1 || true
		if id clicd >/dev/null 2>&1; then
			printf 'startxfce4\n' >/home/clicd/.xsession || true
			chown clicd:clicd /home/clicd/.xsession >/dev/null 2>&1 || true
		fi
		mkdir -p /etc/lightdm/lightdm.conf.d
		cat >/etc/lightdm/lightdm.conf.d/50-clicd-autologin.conf <<'EOF'
[Seat:*]
autologin-user=clicd
autologin-user-timeout=0
user-session=xfce
greeter-session=lightdm-gtk-greeter
EOF
		if [ -x /usr/sbin/lightdm ]; then
			printf '/usr/sbin/lightdm\n' >/etc/X11/default-display-manager || true
		fi
		if command -v systemctl >/dev/null 2>&1; then
			systemctl daemon-reload >/dev/null 2>&1 || true
			systemctl set-default graphical.target >/dev/null 2>&1 || true
			systemctl enable display-manager.service >/dev/null 2>&1 || true
			systemctl enable lightdm.service >/dev/null 2>&1 || true
			systemctl restart lightdm.service >/dev/null 2>&1 || systemctl start lightdm.service >/dev/null 2>&1 || true
		fi
		apt-get clean || true
		echo "CLICD XFCE setup finished at $(date -Is)"
	} || true
fi
`
}

func qemuGuestPing(name string) error {
	out, err := exec.Command("virsh", "qemu-agent-command", name, `{"execute":"guest-ping"}`).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if strings.Contains(msg, "guest agent is not configured") || strings.Contains(msg, "QEMU guest agent is not configured") || strings.Contains(msg, "argument unsupported") {
			return fmt.Errorf("QEMU guest agent is not active for %s; restart the VM once to attach the agent channel, or reinstall if the image was created before KVM SSH initialization support", name)
		}
		return fmt.Errorf("QEMU guest agent is not ready for %s: %v, output: %s", name, err, msg)
	}
	return nil
}

func qemuGuestExec(name string, script string, timeout time.Duration) error {
	return qemuGuestExecCommand(name, "/bin/sh", []string{"-lc", script}, timeout)
}

func qemuGuestExecCommand(name string, path string, args []string, timeout time.Duration) error {
	_, _, err := qemuGuestExecCommandOutput(name, path, args, timeout)
	return err
}

func qemuGuestExecCommandOutput(name string, path string, args []string, timeout time.Duration) (string, string, error) {
	req := map[string]interface{}{
		"execute": "guest-exec",
		"arguments": map[string]interface{}{
			"path":           path,
			"arg":            args,
			"capture-output": true,
		},
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return "", "", err
	}
	out, err := exec.Command("virsh", "qemu-agent-command", name, string(payload)).CombinedOutput()
	if err != nil {
		return "", "", fmt.Errorf("guest-exec failed: %v, output: %s", err, string(out))
	}
	var started struct {
		Return struct {
			PID int `json:"pid"`
		} `json:"return"`
	}
	if err := json.Unmarshal(out, &started); err != nil || started.Return.PID <= 0 {
		return "", "", fmt.Errorf("guest-exec returned invalid response: %s", string(out))
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		statusReq := fmt.Sprintf(`{"execute":"guest-exec-status","arguments":{"pid":%d}}`, started.Return.PID)
		statusOut, err := exec.Command("virsh", "qemu-agent-command", name, statusReq).CombinedOutput()
		if err != nil {
			return "", "", fmt.Errorf("guest-exec-status failed: %v, output: %s", err, string(statusOut))
		}
		var status struct {
			Return struct {
				Exited   bool   `json:"exited"`
				Exitcode int    `json:"exitcode"`
				OutData  string `json:"out-data"`
				ErrData  string `json:"err-data"`
			} `json:"return"`
		}
		if err := json.Unmarshal(statusOut, &status); err != nil {
			return "", "", fmt.Errorf("guest-exec-status returned invalid response: %s", string(statusOut))
		}
		if !status.Return.Exited {
			time.Sleep(3 * time.Second)
			continue
		}
		stdoutBytes, _ := base64.StdEncoding.DecodeString(status.Return.OutData)
		stderrBytes, _ := base64.StdEncoding.DecodeString(status.Return.ErrData)
		stdout := string(stdoutBytes)
		stderr := string(stderrBytes)
		if status.Return.Exitcode == 0 {
			return stdout, stderr, nil
		}
		return stdout, stderr, fmt.Errorf("guest command exited with %d, stdout: %s, stderr: %s", status.Return.Exitcode, stdout, stderr)
	}
	return "", "", fmt.Errorf("timed out waiting for guest command after %s", timeout)
}

func (m *Manager) getUsageCounters(c *config.Container) (uint64, uint64, uint64, uint64, uint64) {
	if c == nil || c.Status != "running" {
		return 0, 0, 0, 0, 0
	}
	name := c.VirshName()
	cpuUsec, readBytes, writeBytes := virshDomstatsCounters(name)
	rxBytes, txBytes := virshInterfaceBytes(name, c.MACAddress)
	return cpuUsec, rxBytes, txBytes, readBytes, writeBytes
}

func (m *Manager) StartUsageMonitor() {
	go func() {
		for {
			time.Sleep(5 * time.Second)
			m.updateAllRates()
		}
	}()
}

func (m *Manager) StartIPv6Guard() {
	go func() {
		m.applyIPv6Guards()
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			m.applyIPv6Guards()
		}
	}()
}

func (m *Manager) StartNetworkSyncMonitor() {
	go func() {
		m.syncRunningNetworks()
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			m.syncRunningNetworks()
		}
	}()
}

func (m *Manager) syncRunningNetworks() {
	for i := range config.AppConfig.Containers {
		c := &config.AppConfig.Containers[i]
		if !c.IsKVM() {
			continue
		}
		status, err := m.GetContainerStatus(c.VirshName())
		if err == nil && status != "" && c.Status != status {
			c.Status = status
			config.SaveConfig()
		}
		if status != "running" && c.Status != "running" {
			continue
		}
		if _, err := m.RefreshVNCPort(c.ID); err != nil {
			fmt.Printf("Warning: failed to sync VNC port for %s: %v\n", c.Name, err)
		}
		if ip, err := m.RefreshNetwork(c.ID); err == nil && ip != "" {
			c.IP = ip
		} else if err != nil {
			fmt.Printf("Warning: failed to sync KVM network for %s: %v\n", c.Name, err)
		}
		if c.IPv6 != "" || len(c.IPv6Addresses) > 0 {
			if err := m.applyIPv6Runtime(c); err != nil {
				fmt.Printf("Warning: failed to sync KVM IPv6 for %s: %v\n", c.Name, err)
			}
		}
	}
}

func (m *Manager) applyIPv6Guards() {
	for i := range config.AppConfig.Containers {
		c := &config.AppConfig.Containers[i]
		if !c.IsKVM() || c.MACAddress == "" {
			continue
		}
		if c.IPv6 == "" && len(c.IPv6Addresses) == 0 {
			ensureKVMIPv6DenyRule("virbr0", c.MACAddress)
			continue
		}
		if err := m.applyIPv6HostRuntime(c); err != nil {
			fmt.Printf("Warning: failed to refresh KVM IPv6 guard for %s: %v\n", c.Name, err)
			continue
		}
		if c.Status == "running" {
			_ = m.applyGuestIPv6Runtime(c)
		}
	}
}

func (m *Manager) updateAllRates() {
	usageMu.Lock()
	defer usageMu.Unlock()

	for i := range config.AppConfig.Containers {
		c := &config.AppConfig.Containers[i]
		if !c.IsKVM() {
			continue
		}
		name := c.VirshName()
		if c.Status != "running" {
			delete(lastUsage, name)
			delete(rateCache, name)
			continue
		}
		cpuUsec, rxBytes, txBytes, readBytes, writeBytes := m.getUsageCounters(c)
		now := time.Now()
		sample := usageSample{CPUUsec: cpuUsec, RXBytes: rxBytes, TXBytes: txBytes, ReadBytes: readBytes, WriteBytes: writeBytes, At: now}
		prev, exists := lastUsage[name]
		lastUsage[name] = sample
		rate := rateSnapshot{UpdatedAt: now}
		if exists {
			elapsed := sample.At.Sub(prev.At).Seconds()
			if elapsed > 0 {
				if sample.CPUUsec >= prev.CPUUsec {
					rate.CPUPct = float64(sample.CPUUsec-prev.CPUUsec) / (elapsed * 1e6) * 100
				}
				if sample.RXBytes >= prev.RXBytes {
					rate.RXBps = float64(sample.RXBytes-prev.RXBytes) / elapsed
				}
				if sample.TXBytes >= prev.TXBytes {
					rate.TXBps = float64(sample.TXBytes-prev.TXBytes) / elapsed
				}
				if sample.ReadBytes >= prev.ReadBytes {
					rate.ReadBps = float64(sample.ReadBytes-prev.ReadBytes) / elapsed
				}
				if sample.WriteBytes >= prev.WriteBytes {
					rate.WriteBps = float64(sample.WriteBytes-prev.WriteBytes) / elapsed
				}
			}
		}
		rateCache[name] = rate
	}
}

func virshDomstatsCounters(name string) (uint64, uint64, uint64) {
	out, err := exec.Command("virsh", "domstats", name, "--cpu-total", "--block").Output()
	if err != nil {
		return 0, 0, 0
	}
	var cpuUsec, readBytes, writeBytes uint64
	for _, line := range strings.Split(string(out), "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), "=", 2)
		if len(parts) != 2 {
			continue
		}
		key, value := parts[0], parts[1]
		parsed, _ := strconv.ParseUint(value, 10, 64)
		switch {
		case key == "cpu.time":
			cpuUsec = parsed / 1000
		case strings.HasSuffix(key, ".rd.bytes"):
			readBytes += parsed
		case strings.HasSuffix(key, ".wr.bytes"):
			writeBytes += parsed
		}
	}
	return cpuUsec, readBytes, writeBytes
}

func virshInterfaceBytes(name string, mac string) (uint64, uint64) {
	iface := virshInterfaceName(name, mac)
	if iface == "" {
		return 0, 0
	}
	out, err := exec.Command("virsh", "domifstat", name, iface).Output()
	if err != nil {
		return 0, 0
	}
	var rx, tx uint64
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		key := fields[0]
		valueField := fields[len(fields)-1]
		if len(fields) >= 3 && strings.HasPrefix(fields[0], iface) {
			key = fields[1]
			valueField = fields[2]
		}
		value, _ := strconv.ParseUint(valueField, 10, 64)
		switch key {
		case "rx_bytes":
			rx = value
		case "tx_bytes":
			tx = value
		}
	}
	return rx, tx
}

func virshInterfaceName(name string, mac string) string {
	out, err := exec.Command("virsh", "domiflist", name).Output()
	if err != nil {
		return ""
	}
	mac = strings.ToLower(mac)
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 || strings.HasPrefix(fields[0], "-") || strings.EqualFold(fields[0], "Interface") {
			continue
		}
		if mac == "" || strings.EqualFold(fields[4], mac) {
			return fields[0]
		}
	}
	return ""
}

func (m *Manager) GetTrafficInfo(id int) map[string]interface{} {
	c := config.FindContainer(id)
	if c == nil {
		return nil
	}
	m.accumulateContainerTraffic(c)
	return trafficInfoMap(c)
}

func (m *Manager) AccumulateTraffic() {
	currentMonth := time.Now().Format("2006-01")
	changed := false
	trafficMu.Lock()
	defer trafficMu.Unlock()
	for i := range config.AppConfig.Containers {
		c := &config.AppConfig.Containers[i]
		if !c.IsKVM() {
			continue
		}
		if c.TrafficResetDate != currentMonth {
			c.TrafficUsedRX = 0
			c.TrafficUsedTX = 0
			c.TrafficResetDate = currentMonth
			delete(lastTrafficSnapshot, c.VirshName())
			changed = true
		}
		if c.Status != "running" {
			delete(lastTrafficSnapshot, c.VirshName())
			continue
		}
		if accumulateTrafficLocked(c) {
			changed = true
		}
	}
	if changed {
		config.SaveConfig()
	}
}

func (m *Manager) accumulateContainerTraffic(c *config.Container) {
	currentMonth := time.Now().Format("2006-01")
	trafficMu.Lock()
	defer trafficMu.Unlock()
	changed := false
	if c.TrafficResetDate != currentMonth {
		c.TrafficUsedRX = 0
		c.TrafficUsedTX = 0
		c.TrafficResetDate = currentMonth
		delete(lastTrafficSnapshot, c.VirshName())
		changed = true
	}
	if c.Status == "running" && accumulateTrafficLocked(c) {
		changed = true
	}
	if changed {
		config.SaveConfig()
	}
}

func accumulateTrafficLocked(c *config.Container) bool {
	rx, tx := virshInterfaceBytes(c.VirshName(), c.MACAddress)
	key := c.VirshName()
	prev, exists := lastTrafficSnapshot[key]
	changed := false
	if exists && rx >= prev.RXBytes && tx >= prev.TXBytes {
		deltaRX := int64(rx - prev.RXBytes)
		deltaTX := int64(tx - prev.TXBytes)
		if deltaRX > 0 || deltaTX > 0 {
			c.TrafficUsedRX += deltaRX
			c.TrafficUsedTX += deltaTX
			changed = true
		}
	}
	lastTrafficSnapshot[key] = trafficSample{RXBytes: rx, TXBytes: tx}
	return changed
}

func trafficInfoMap(c *config.Container) map[string]interface{} {
	totalUsed := c.TrafficUsedRX + c.TrafficUsedTX
	limitGB := 0
	usedPct := 0.0
	if c.TrafficMode == "in_out" {
		limitGB = c.TrafficInGB + c.TrafficOutGB
		inLimit := float64(c.TrafficInGB) * 1073741824
		outLimit := float64(c.TrafficOutGB) * 1073741824
		if c.TrafficInGB > 0 {
			usedPct = float64(c.TrafficUsedRX) / inLimit * 100
		}
		if c.TrafficOutGB > 0 {
			outPct := float64(c.TrafficUsedTX) / outLimit * 100
			if outPct > usedPct {
				usedPct = outPct
			}
		}
	} else {
		limitGB = c.MonthlyTrafficGB
		if limitGB > 0 {
			usedPct = float64(totalUsed) / float64(limitGB*1073741824) * 100
		}
	}
	return map[string]interface{}{
		"total_used_bytes": totalUsed,
		"rx_used_bytes":    c.TrafficUsedRX,
		"tx_used_bytes":    c.TrafficUsedTX,
		"mode":             c.TrafficMode,
		"limit_gb":         limitGB,
		"in_limit_gb":      c.TrafficInGB,
		"out_limit_gb":     c.TrafficOutGB,
		"used_pct":         usedPct,
		"reset_date":       c.TrafficResetDate,
	}
}

func (m *Manager) StartExpiryScanner() {
	go func() {
		for {
			time.Sleep(30 * time.Second)
			now := time.Now()
			m.AccumulateTraffic()
			m.StopExpiredContainers(now)
			m.StopTrafficExceededContainers(now)
		}
	}()
}

func (m *Manager) StopExpiredContainers(now time.Time) {
	for _, container := range config.AppConfig.Containers {
		if !container.IsKVM() || !lxc.IsExpired(container) {
			continue
		}
		status, err := m.GetContainerStatus(container.VirshName())
		if err != nil {
			status = container.Status
		}
		if status != "running" {
			continue
		}
		fmt.Printf("KVM VM %s (ID=%d) expired at %s, stopping...\n", container.Name, container.ID, container.ExpiresAt)
		if err := m.StopContainer(container.ID); err != nil {
			fmt.Printf("Warning: failed to stop expired KVM VM %s: %v\n", container.Name, err)
		}
	}
}

func (m *Manager) StopTrafficExceededContainers(now time.Time) {
	currentMonth := now.Format("2006-01")
	saved := false
	for i := range config.AppConfig.Containers {
		c := &config.AppConfig.Containers[i]
		if !c.IsKVM() || c.Status != "running" {
			continue
		}
		if c.TrafficResetDate != currentMonth {
			c.TrafficUsedRX = 0
			c.TrafficUsedTX = 0
			c.TrafficResetDate = currentMonth
			saved = true
			continue
		}
		if lxc.IsTrafficExceeded(*c) {
			fmt.Printf("KVM VM %s (ID=%d) exceeded traffic limit, stopping...\n", c.Name, c.ID)
			if err := m.StopContainer(c.ID); err != nil {
				fmt.Printf("Warning: failed to stop traffic-exceeded KVM VM %s: %v\n", c.Name, err)
			}
		}
	}
	if saved {
		config.SaveConfig()
	}
}

func virshMemBytes(name string) int64 {
	out, err := exec.Command("virsh", "dommemstat", name).Output()
	if err != nil {
		return 0
	}
	stats := map[string]int64{}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		kib, err := strconv.ParseInt(fields[1], 10, 64)
		if err == nil {
			stats[fields[0]] = kib
		}
	}
	if available := stats["available"]; available > 0 {
		if unused, ok := stats["unused"]; ok && unused >= 0 && available >= unused {
			return (available - unused) * 1024
		}
	}
	if actual := stats["actual"]; actual > 0 {
		if unused, ok := stats["unused"]; ok && unused > 0 && actual >= unused {
			return (actual - unused) * 1024
		}
	}
	if rss := stats["rss"]; rss > 0 {
		return rss * 1024
	}
	return 0
}

func (m *Manager) AssignIPv6(id int) (*config.Container, error) {
	c := config.FindContainer(id)
	if c == nil {
		return nil, fmt.Errorf("container not found: %d", id)
	}
	if !c.IsKVM() {
		return nil, fmt.Errorf("container is not a KVM VM: %d", id)
	}
	if c.IPv6 == "" {
		assignments, err := m.allocateIPv6AssignmentsForContainer(id, nil, 1, true)
		if err != nil {
			return nil, err
		}
		c.IPv6Addresses = append(c.IPv6Addresses, assignments...)
		c.NormalizeNetworkAssignments()
		config.SaveConfig()
	}
	if err := m.applyIPv6Runtime(c); err != nil {
		return nil, err
	}
	return c, nil
}

func (m *Manager) UpdatePublicIPv4Assignments(id int, requested []string, count int, auto bool) (*config.Container, error) {
	c := config.FindContainer(id)
	if c == nil {
		return nil, fmt.Errorf("container not found: %d", id)
	}
	if !c.IsKVM() {
		return nil, fmt.Errorf("container is not a KVM VM: %d", id)
	}

	assignments := []config.PublicIPv4Assignment{}
	if auto || len(requested) > 0 {
		allocated, err := lxc.AllocatePublicIPv4Assignments(id, requested, count, auto)
		if err != nil {
			return nil, err
		}
		assignments = allocated
	}

	c.PublicIPv4s = assignments
	reconcileKVMPortMappingHostIPs(c)
	c.NormalizeNetworkAssignments()
	config.SaveConfig()

	lxcManager := lxc.NewManager()
	_ = lxcManager.CleanPortMappings(id)
	lxc.EnsureAssignedPublicIPv4s(c.PublicIPv4s)
	if c.Status == "running" && c.IP != "" {
		if err := lxcManager.ApplyPortMappings(id); err != nil {
			return nil, err
		}
	}
	return c, nil
}

func reconcileKVMPortMappingHostIPs(c *config.Container) {
	if c == nil {
		return
	}
	assigned := map[string]bool{}
	for _, item := range c.PublicIPv4s {
		if addr := strings.TrimSpace(item.Address); addr != "" {
			assigned[addr] = true
		}
	}
	replacement := ""
	if len(assigned) == 1 {
		for addr := range assigned {
			replacement = addr
		}
	}
	for i := range c.PortMappings {
		hostIP := strings.TrimSpace(c.PortMappings[i].HostIP)
		if hostIP == "" || assigned[hostIP] {
			continue
		}
		c.PortMappings[i].HostIP = replacement
	}
}

func (m *Manager) UpdateIPv6Assignments(id int, requested []string, count int, auto bool) (*config.Container, error) {
	c := config.FindContainer(id)
	if c == nil {
		return nil, fmt.Errorf("container not found: %d", id)
	}
	if !c.IsKVM() {
		return nil, fmt.Errorf("container is not a KVM VM: %d", id)
	}

	old := *c
	old.IPv6Addresses = append([]config.IPv6Assignment(nil), c.IPv6Addresses...)
	removeKVMIPv6Runtime(&old)

	assignments := []config.IPv6Assignment{}
	if auto || len(requested) > 0 {
		allocated, err := m.allocateIPv6AssignmentsForContainer(id, requested, count, auto)
		if err != nil {
			if old.IPv6 != "" || len(old.IPv6Addresses) > 0 {
				_ = m.applyIPv6Runtime(&old)
			}
			return nil, err
		}
		assignments = allocated
	}

	c.IPv6 = ""
	c.IPv6PrefixLen = 0
	c.IPv6Interface = ""
	c.IPv6Addresses = assignments
	c.NormalizeNetworkAssignments()
	config.SaveConfig()

	if len(c.IPv6Addresses) > 0 {
		if err := m.applyIPv6Runtime(c); err != nil {
			return nil, err
		}
	} else if c.Status == "running" {
		if err := lxc.ApplyFirewallRules(c.ID); err != nil {
			fmt.Printf("Warning: failed to re-apply firewall rules after KVM IPv6 removal for %s: %v\n", c.Name, err)
		}
	}
	return c, nil
}

func (m *Manager) applyIPv6Runtime(c *config.Container) error {
	if c == nil || (c.IPv6 == "" && len(c.IPv6Addresses) == 0) {
		return nil
	}
	c.NormalizeNetworkAssignments()
	if err := m.applyIPv6HostRuntime(c); err != nil {
		return err
	}
	if c.Status == "running" {
		if err := m.applyGuestIPv6Runtime(c); err != nil {
			if shouldLogIPv6GuestWarning(c.ID) {
				fmt.Printf("Warning: failed to apply KVM guest IPv6 for %s: %v\n", c.Name, err)
			}
		}
	}
	for _, assignment := range c.IPv6Addresses {
		uplink := assignment.Interface
		if uplink == "" {
			uplink = c.IPv6Interface
		}
		ensureKVMIPv6NAT66(assignment.Address, uplink)
	}
	if c.Status == "running" {
		if err := lxc.ApplyFirewallRules(c.ID); err != nil {
			fmt.Printf("Warning: failed to re-apply firewall rules after KVM IPv6 setup for %s: %v\n", c.Name, err)
		}
	}
	return nil
}

func shouldLogIPv6GuestWarning(id int) bool {
	ipv6WarnMu.Lock()
	defer ipv6WarnMu.Unlock()
	now := time.Now()
	if last, ok := lastIPv6GuestWarn[id]; ok && now.Sub(last) < 5*time.Minute {
		return false
	}
	lastIPv6GuestWarn[id] = now
	return true
}

func (m *Manager) applyIPv6HostRuntime(c *config.Container) error {
	if c == nil || (c.IPv6 == "" && len(c.IPv6Addresses) == 0) {
		return nil
	}
	c.NormalizeNetworkAssignments()
	if c.IPv6Interface == "" {
		prefixes := lxc.DetectPublicIPv6Prefixes()
		if len(prefixes) == 0 {
			return fmt.Errorf("failed to detect IPv6 uplink for %s", c.IPv6)
		}
		c.IPv6Interface = prefixes[0].Interface
		c.IPv6PrefixLen = prefixes[0].PrefixLen
		for i := range c.IPv6Addresses {
			if c.IPv6Addresses[i].Interface == "" {
				c.IPv6Addresses[i].Interface = c.IPv6Interface
			}
			if c.IPv6Addresses[i].PrefixLen == 0 {
				c.IPv6Addresses[i].PrefixLen = c.IPv6PrefixLen
			}
		}
		config.SaveConfig()
	}
	runQuiet("sysctl", "-w", "net.ipv6.conf.all.forwarding=1")
	runQuiet("sysctl", "-w", "net.ipv6.conf."+c.IPv6Interface+".accept_ra=2")
	runQuiet("sysctl", "-w", "net.ipv6.conf."+c.IPv6Interface+".proxy_ndp=1")
	bridge := "virbr0"
	runQuiet("sysctl", "-w", "net.ipv6.conf."+bridge+".disable_ipv6=0")
	runQuiet("sysctl", "-w", "net.ipv6.conf."+bridge+".forwarding=1")
	runQuiet("sysctl", "-w", "net.ipv6.conf."+bridge+".proxy_ndp=1")
	runQuiet("ip", "link", "set", bridge, "up")
	runQuiet("ip", "-6", "addr", "replace", ipv6GatewayLinkLocal+"/64", "dev", bridge)
	for _, assignment := range c.IPv6Addresses {
		uplink := assignment.Interface
		if uplink == "" {
			uplink = c.IPv6Interface
		}
		if out, err := exec.Command("ip", "-6", "route", "replace", assignment.Address+"/128", "dev", bridge).CombinedOutput(); err != nil {
			return fmt.Errorf("failed to add IPv6 VM route: %v, output: %s", err, string(out))
		}
		if out, err := exec.Command("ip", "-6", "neigh", "replace", "proxy", assignment.Address, "dev", uplink).CombinedOutput(); err != nil {
			return fmt.Errorf("failed to add IPv6 proxy NDP: %v, output: %s", err, string(out))
		}
		ensureKVMIPv6ForwardRules(assignment.Address, bridge)
		ensureKVMIPv6AntiSpoofRules(assignment.Address, bridge, c.MACAddress)
	}
	return nil
}

func runQuiet(name string, args ...string) {
	_ = exec.Command(name, args...).Run()
}

func ensureKVMIPv6ForwardRules(ipv6 string, bridge string) {
	if ipv6 == "" || bridge == "" {
		return
	}
	rules := [][]string{
		{"FORWARD", "-i", bridge, "-s", ipv6 + "/128", "-j", "ACCEPT"},
		{"FORWARD", "-o", bridge, "-d", ipv6 + "/128", "-j", "ACCEPT"},
	}
	for _, rule := range rules {
		check := append([]string{"-C"}, rule...)
		add := append([]string{"-A"}, rule...)
		if exec.Command("ip6tables", check...).Run() != nil {
			exec.Command("ip6tables", add...).Run()
		}
	}
}

func ensureKVMIPv6AntiSpoofRules(ipv6 string, bridge string, mac string) {
	if ipv6 == "" || bridge == "" || mac == "" {
		return
	}
	mac = strings.ToLower(strings.TrimSpace(mac))
	acceptRule := []string{"FORWARD", "-i", bridge, "-m", "mac", "--mac-source", mac, "-s", ipv6 + "/128", "-j", "ACCEPT"}
	dropRule := []string{"FORWARD", "-i", bridge, "-m", "mac", "--mac-source", mac, "-j", "DROP"}
	deleteIP6Rule(acceptRule)
	deleteIP6Rule(dropRule)
	insertIP6Rule(dropRule)
	insertIP6Rule(acceptRule)
}

func ensureKVMIPv6DenyRule(bridge string, mac string) {
	if bridge == "" || mac == "" {
		return
	}
	mac = strings.ToLower(strings.TrimSpace(mac))
	rule := []string{"FORWARD", "-i", bridge, "-m", "mac", "--mac-source", mac, "-j", "DROP"}
	deleteIP6Rule(rule)
	insertIP6Rule(rule)
}

func ensureKVMIPv6NAT66(ipv6 string, uplink string) {
	if ipv6 == "" || uplink == "" {
		return
	}
	rule := []string{"POSTROUTING", "-s", ipv6 + "/128", "-o", uplink, "-j", "MASQUERADE"}
	check := append([]string{"-t", "nat", "-C"}, rule...)
	add := append([]string{"-t", "nat", "-I"}, append([]string{rule[0], "1"}, rule[1:]...)...)
	if exec.Command("ip6tables", check...).Run() != nil {
		exec.Command("ip6tables", add...).Run()
	}
}

func removeKVMIPv6Runtime(c *config.Container) {
	if c == nil {
		return
	}
	bridge := "virbr0"
	removeKVMIPv6DenyRule(bridge, c.MACAddress)
	if c.IPv6 == "" && len(c.IPv6Addresses) == 0 {
		return
	}
	c.NormalizeNetworkAssignments()
	for _, assignment := range c.IPv6Addresses {
		uplink := assignment.Interface
		if uplink == "" {
			uplink = c.IPv6Interface
		}
		removeKVMIPv6NAT66(assignment.Address, uplink)
		removeKVMIPv6ForwardRules(assignment.Address, bridge)
		removeKVMIPv6AntiSpoofRules(assignment.Address, bridge, c.MACAddress)
		if uplink != "" {
			_ = exec.Command("ip", "-6", "neigh", "del", "proxy", assignment.Address, "dev", uplink).Run()
		}
		_ = exec.Command("ip", "-6", "route", "del", assignment.Address+"/128", "dev", bridge).Run()
	}
}

func removeKVMIPv6ForwardRules(ipv6 string, bridge string) {
	if ipv6 == "" || bridge == "" {
		return
	}
	rules := [][]string{
		{"FORWARD", "-i", bridge, "-s", ipv6 + "/128", "-j", "ACCEPT"},
		{"FORWARD", "-o", bridge, "-d", ipv6 + "/128", "-j", "ACCEPT"},
	}
	for _, rule := range rules {
		deleteIP6Rule(rule)
	}
}

func removeKVMIPv6AntiSpoofRules(ipv6 string, bridge string, mac string) {
	if ipv6 == "" || bridge == "" || mac == "" {
		return
	}
	mac = strings.ToLower(strings.TrimSpace(mac))
	deleteIP6Rule([]string{"FORWARD", "-i", bridge, "-m", "mac", "--mac-source", mac, "-s", ipv6 + "/128", "-j", "ACCEPT"})
	deleteIP6Rule([]string{"FORWARD", "-i", bridge, "-m", "mac", "--mac-source", mac, "-j", "DROP"})
}

func removeKVMIPv6DenyRule(bridge string, mac string) {
	if bridge == "" || mac == "" {
		return
	}
	mac = strings.ToLower(strings.TrimSpace(mac))
	deleteIP6Rule([]string{"FORWARD", "-i", bridge, "-m", "mac", "--mac-source", mac, "-j", "DROP"})
}

func removeKVMIPv6NAT66(ipv6 string, uplink string) {
	if ipv6 == "" || uplink == "" {
		return
	}
	rule := []string{"POSTROUTING", "-s", ipv6 + "/128", "-o", uplink, "-j", "MASQUERADE"}
	for {
		del := append([]string{"-t", "nat", "-D"}, rule...)
		if exec.Command("ip6tables", del...).Run() != nil {
			return
		}
	}
}

func insertIP6Rule(rule []string) {
	if len(rule) == 0 {
		return
	}
	add := append([]string{"-I"}, append([]string{rule[0], "1"}, rule[1:]...)...)
	_ = exec.Command("ip6tables", add...).Run()
}

func deleteIP6Rule(rule []string) {
	for {
		del := append([]string{"-D"}, rule...)
		if exec.Command("ip6tables", del...).Run() != nil {
			return
		}
	}
}

func (m *Manager) applyGuestIPv6(c *config.Container) error {
	if c == nil || (c.IPv6 == "" && len(c.IPv6Addresses) == 0) {
		return nil
	}
	c.NormalizeNetworkAssignments()
	if IsWindowsImage(c.Template) {
		return m.applyWindowsGuestIPv6(c)
	}
	script := kvmIPv6SetupScript(c.IPv6AddressStrings())
	if err := qemuGuestPing(c.VirshName()); err != nil {
		return err
	}
	return qemuGuestExec(c.VirshName(), script, 60*time.Second)
}

func (m *Manager) applyWindowsGuestIPv6(c *config.Container) error {
	if c == nil || (c.IPv6 == "" && len(c.IPv6Addresses) == 0) {
		return nil
	}
	if err := qemuGuestPing(c.VirshName()); err != nil {
		return err
	}
	c.NormalizeNetworkAssignments()
	script := windowsIPv6PowerShell(c.IPv6AddressStrings(), c.MACAddress)
	return qemuGuestExecCommand(c.VirshName(), "powershell.exe", []string{"-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", script}, 60*time.Second)
}

func (m *Manager) applyGuestIPv6Runtime(c *config.Container) error {
	if c == nil || (c.IPv6 == "" && len(c.IPv6Addresses) == 0) {
		return nil
	}
	c.NormalizeNetworkAssignments()
	qgaErr := m.applyGuestIPv6(c)
	if qgaErr == nil {
		return nil
	}
	sshErr := m.applyGuestIPv6OverSSH(c)
	if sshErr == nil {
		return nil
	}
	return fmt.Errorf("QGA: %v; SSH: %v", qgaErr, sshErr)
}

func (m *Manager) applyGuestIPv6OverSSH(c *config.Container) error {
	if c == nil || (c.IPv6 == "" && len(c.IPv6Addresses) == 0) {
		return nil
	}
	if IsWindowsImage(c.Template) {
		return fmt.Errorf("SSH IPv6 fallback is not supported for Windows guests")
	}
	if c.IP == "" || c.SSHPassword == "" {
		return fmt.Errorf("missing guest IPv4 or SSH password")
	}
	client, err := ssh.Dial("tcp", net.JoinHostPort(c.IP, "22"), &ssh.ClientConfig{
		User:            "root",
		Auth:            []ssh.AuthMethod{ssh.Password(c.SSHPassword)},
		HostKeyCallback: kvmHostKeyCallback(c),
		Timeout:         8 * time.Second,
	})
	if err != nil {
		return err
	}
	defer client.Close()
	return runKVMSSHScript(client, kvmIPv6SetupScript(c.IPv6AddressStrings()), "KVM IPv6", 60*time.Second)
}

func kvmIPv6SetupScript(ipv6s []string) string {
	ipv6s = normalizeKVMIPv6List(ipv6s)
	return `set -eu
IPV6_ADDRS="` + strings.Join(ipv6s, " ") + `"
IPV6_GW=` + shellQuote(ipv6GatewayLinkLocal) + `
IFACE="$(ip -o -4 route show default 2>/dev/null | awk '{print $5; exit}')"
if [ -z "$IFACE" ]; then
	IFACE="$(ip -o link show up | awk -F': ' '$2 != "lo" {print $2; exit}' | cut -d@ -f1)"
fi
if [ -z "$IFACE" ]; then
	echo "failed to detect guest network interface" >&2
	exit 1
fi
sysctl -w net.ipv6.conf.all.disable_ipv6=0 >/dev/null 2>&1 || true
sysctl -w net.ipv6.conf.default.disable_ipv6=0 >/dev/null 2>&1 || true
sysctl -w net.ipv6.conf."$IFACE".disable_ipv6=0 >/dev/null 2>&1 || true
for IPV6_ADDR in $IPV6_ADDRS; do
	ip -6 addr replace "$IPV6_ADDR/128" dev "$IFACE"
done
ip -6 route replace default via "$IPV6_GW" dev "$IFACE" onlink metric 100
mkdir -p /usr/local/sbin /etc/systemd/system /etc/network/if-up.d /etc/local.d
cat > /usr/local/sbin/clicd-kvm-ipv6-init <<'EOF'
#!/bin/sh
set -eu
IPV6_ADDRS="` + strings.Join(ipv6s, " ") + `"
IPV6_GW=` + shellQuote(ipv6GatewayLinkLocal) + `
IFACE="$(ip -o -4 route show default 2>/dev/null | awk '{print $5; exit}')"
if [ -z "$IFACE" ]; then
	IFACE="$(ip -o link show up | awk -F': ' '$2 != "lo" {print $2; exit}' | cut -d@ -f1)"
fi
[ -n "$IFACE" ] || exit 0
sysctl -w net.ipv6.conf.all.disable_ipv6=0 >/dev/null 2>&1 || true
sysctl -w net.ipv6.conf.default.disable_ipv6=0 >/dev/null 2>&1 || true
sysctl -w net.ipv6.conf."$IFACE".disable_ipv6=0 >/dev/null 2>&1 || true
for IPV6_ADDR in $IPV6_ADDRS; do
	ip -6 addr replace "$IPV6_ADDR/128" dev "$IFACE"
done
ip -6 route replace default via "$IPV6_GW" dev "$IFACE" onlink metric 100
EOF
chmod +x /usr/local/sbin/clicd-kvm-ipv6-init
cat > /etc/systemd/system/clicd-kvm-ipv6.service <<'EOF'
[Unit]
Description=CLICD KVM IPv6 setup
After=network-online.target
Wants=network-online.target

[Service]
Type=oneshot
ExecStart=/usr/local/sbin/clicd-kvm-ipv6-init
RemainAfterExit=yes

[Install]
WantedBy=multi-user.target
EOF
systemctl daemon-reload >/dev/null 2>&1 || true
systemctl enable --now clicd-kvm-ipv6.service >/dev/null 2>&1 || true
cat > /etc/local.d/clicd-kvm-ipv6.start <<'EOF'
#!/bin/sh
/usr/local/sbin/clicd-kvm-ipv6-init || true
EOF
chmod +x /etc/local.d/clicd-kvm-ipv6.start
if command -v rc-update >/dev/null 2>&1; then
	rc-update add local default >/dev/null 2>&1 || true
	rc-service local restart >/dev/null 2>&1 || true
fi
cat > /etc/network/if-up.d/clicd-kvm-ipv6 <<'EOF'
#!/bin/sh
[ "$IFACE" = "lo" ] && exit 0
/usr/local/sbin/clicd-kvm-ipv6-init || true
EOF
chmod +x /etc/network/if-up.d/clicd-kvm-ipv6
`
}

func (m *Manager) allocateIPv6ForContainer(id int) (string, int, string, error) {
	assignments, err := m.allocateIPv6AssignmentsForContainer(id, nil, 1, true)
	if err != nil {
		return "", 0, "", err
	}
	if len(assignments) == 0 {
		return "", 0, "", fmt.Errorf("no free IPv6 address")
	}
	return assignments[0].Address, assignments[0].PrefixLen, assignments[0].Interface, nil
}

func (m *Manager) allocateIPv6AssignmentsForContainer(id int, requested []string, count int, auto bool) ([]config.IPv6Assignment, error) {
	var err error
	count, err = lxc.NormalizePublicIPAllocationCount(requested, count)
	if err != nil {
		return nil, err
	}
	prefixes := lxc.DetectPublicIPv6Prefixes()
	if len(prefixes) == 0 {
		return nil, fmt.Errorf("public IPv6 allocation is unavailable: no usable public IPv6 prefix found")
	}
	parsedPrefixes := make([]struct {
		info   lxc.IPv6PrefixInfo
		prefix netip.Prefix
	}, 0, len(prefixes))
	for _, prefixInfo := range prefixes {
		prefix, err := netip.ParsePrefix(prefixInfo.Prefix)
		if err != nil {
			continue
		}
		parsedPrefixes = append(parsedPrefixes, struct {
			info   lxc.IPv6PrefixInfo
			prefix netip.Prefix
		}{info: prefixInfo, prefix: prefix})
	}
	if len(parsedPrefixes) == 0 {
		return nil, fmt.Errorf("public IPv6 allocation is unavailable: no valid IPv6 prefix found")
	}

	used := map[string]bool{}
	hostAddrs := map[string]bool{}
	for _, p := range prefixes {
		hostAddrs[p.Address] = true
	}
	for _, c := range config.AppConfig.Containers {
		if c.ID == id {
			continue
		}
		if c.IPv6 != "" {
			used[c.IPv6] = true
		}
		for _, ip := range c.IPv6Addresses {
			if ip.Address != "" {
				used[ip.Address] = true
			}
		}
	}
	result := []config.IPv6Assignment{}
	selected := map[string]bool{}
	for _, raw := range requested {
		raw = strings.TrimSpace(raw)
		if raw == "" || selected[raw] {
			continue
		}
		addr, err := netip.ParseAddr(raw)
		if err != nil || !addr.Is6() {
			return nil, fmt.Errorf("requested IPv6 %s is not valid", raw)
		}
		matchIndex := -1
		for i, item := range parsedPrefixes {
			if item.prefix.Contains(addr) {
				matchIndex = i
				break
			}
		}
		if matchIndex < 0 {
			return nil, fmt.Errorf("requested IPv6 %s is not in the configured IPv6 prefixes", raw)
		}
		if hostAddrs[raw] {
			return nil, fmt.Errorf("requested IPv6 %s is used by host", raw)
		}
		if used[raw] {
			return nil, fmt.Errorf("requested IPv6 %s is already assigned", raw)
		}
		selected[raw] = true
		used[raw] = true
		result = append(result, config.IPv6Assignment{Address: raw, PrefixLen: parsedPrefixes[matchIndex].prefix.Bits(), Interface: parsedPrefixes[matchIndex].info.Interface})
	}
	if len(result) >= count || !auto {
		return result, nil
	}
	for _, item := range parsedPrefixes {
		for offset := uint64(0x2000 + id); offset < 0x100000; offset++ {
			addr, err := ipv6Add(item.prefix.Masked().Addr(), offset)
			if err != nil || !item.prefix.Contains(addr) {
				break
			}
			candidate := addr.String()
			if !used[candidate] && !hostAddrs[candidate] {
				used[candidate] = true
				result = append(result, config.IPv6Assignment{Address: candidate, PrefixLen: item.prefix.Bits(), Interface: item.info.Interface})
				if len(result) >= count {
					return result, nil
				}
			}
		}
	}
	return nil, fmt.Errorf("no free IPv6 address in configured prefixes")
}

func ipv6Add(base netip.Addr, offset uint64) (netip.Addr, error) {
	raw := base.As16()
	value := big.NewInt(0).SetBytes(raw[:])
	add := make([]byte, 8)
	binary.BigEndian.PutUint64(add, offset)
	value.Add(value, big.NewInt(0).SetBytes(add))
	bytes := value.Bytes()
	if len(bytes) > 16 {
		return netip.Addr{}, fmt.Errorf("IPv6 address overflow")
	}
	padded := make([]byte, 16)
	copy(padded[16-len(bytes):], bytes)
	var out [16]byte
	copy(out[:], padded)
	return netip.AddrFrom16(out), nil
}

func randomMAC() string {
	b := make([]byte, 3)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("52:54:00:%02x:%02x:%02x", time.Now().UnixNano()&0xff, (time.Now().UnixNano()>>8)&0xff, (time.Now().UnixNano()>>16)&0xff)
	}
	return fmt.Sprintf("52:54:00:%02x:%02x:%02x", b[0], b[1], b[2])
}

func generateRandomString(length int) string {
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())[:length]
	}
	return hex.EncodeToString(b)[:length]
}

func generateWindowsPassword() string {
	upper := "ABCDEFGHJKLMNPQRSTUVWXYZ"
	lower := "abcdefghijkmnopqrstuvwxyz"
	digits := "23456789"
	symbols := "!@#$%*-_+="
	all := upper + lower + digits + symbols
	chars := []byte{
		randomChar(upper),
		randomChar(lower),
		randomChar(digits),
		randomChar(symbols),
	}
	for len(chars) < 20 {
		chars = append(chars, randomChar(all))
	}
	for i := range chars {
		j := secureRandomInt(len(chars))
		chars[i], chars[j] = chars[j], chars[i]
	}
	return string(chars)
}

func randomChar(chars string) byte {
	return chars[secureRandomInt(len(chars))]
}

func secureRandomInt(max int) int {
	if max <= 1 {
		return 0
	}
	n, err := rand.Int(rand.Reader, big.NewInt(int64(max)))
	if err != nil {
		return int(time.Now().UnixNano() % int64(max))
	}
	return int(n.Int64())
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func chpasswdStdin(username, password string) ([]byte, error) {
	if username == "" || strings.ContainsAny(username, ":\n\r") {
		return nil, fmt.Errorf("invalid chpasswd username")
	}
	if strings.ContainsAny(password, "\n\r") {
		return nil, fmt.Errorf("password cannot contain newlines")
	}
	return []byte(username + ":" + password + "\n"), nil
}

func kvmHostKeyCallback(c *config.Container) ssh.HostKeyCallback {
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		return verifyKVMHostKey(c, key, config.SaveConfig)
	}
}

func verifyKVMHostKey(c *config.Container, key ssh.PublicKey, save func() error) error {
	if c == nil {
		return fmt.Errorf("KVM container is nil")
	}
	fingerprint := sshHostKeyFingerprint(key)
	if c.SSHHostKey != "" && c.SSHHostKey != fingerprint {
		return fmt.Errorf("KVM SSH host key mismatch")
	}
	if c.SSHHostKey == "" {
		c.SSHHostKey = fingerprint
		if save != nil {
			if err := save(); err != nil {
				return fmt.Errorf("failed to save KVM SSH host key: %v", err)
			}
		}
	}
	return nil
}

func sshHostKeyFingerprint(key ssh.PublicKey) string {
	sum := sha256.Sum256(key.Marshal())
	return hex.EncodeToString(sum[:])
}

func runStdin(command string, stdin []byte, args ...string) error {
	cmd := exec.Command(command, args...)
	cmd.Stdin = bytes.NewReader(stdin)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s failed: %v, output: %s", command, err, string(output))
	}
	return nil
}
