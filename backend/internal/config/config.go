package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// PortMapping represents a port mapping rule
type PortMapping struct {
	ContainerPort int    `json:"container_port"`
	HostPort      int    `json:"host_port"`
	HostIP        string `json:"host_ip,omitempty"`
	Protocol      string `json:"protocol"`
	Description   string `json:"description"`
}

type FirewallRule struct {
	ID          string `json:"id"`
	Network     string `json:"network,omitempty"` // "ipv4", "ipv6", or "all"; empty defaults to "ipv4"
	Direction   string `json:"direction"`         // "in" or "out"
	Protocol    string `json:"protocol"`          // "tcp", "udp", "icmp", "all"
	Port        string `json:"port"`              // "" = all, "22", "80,443", "8000-9000"
	SourceIP    string `json:"source_ip"`         // "" = any
	Action      string `json:"action"`            // "ACCEPT" or "DROP"
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
}

type PublicIPv4Assignment struct {
	Address   string `json:"address"`
	Interface string `json:"interface,omitempty"`
	PrefixLen int    `json:"prefix_len,omitempty"`
	Gateway   string `json:"gateway,omitempty"`
}

type IPv6Assignment struct {
	Address   string `json:"address"`
	PrefixLen int    `json:"prefix_len"`
	Interface string `json:"interface,omitempty"`
}

type PublicIPv6Prefix struct {
	Address   string `json:"address"`
	Prefix    string `json:"prefix,omitempty"`
	PrefixLen int    `json:"prefix_len"`
	Interface string `json:"interface,omitempty"`
	Gateway   string `json:"gateway,omitempty"`
}

// SavedTask for persisting task queue across restarts
type SavedTask struct {
	ID            string `json:"id"`
	Type          string `json:"type"`
	ContainerID   int    `json:"container_id"`
	ContainerName string `json:"container_name"`
	Status        string `json:"status"`
	Error         string `json:"error,omitempty"`
	CreatedAt     string `json:"created_at"`
	TemplateID    string `json:"template_id,omitempty"`
	Config        string `json:"config,omitempty"`
	User          string `json:"user,omitempty"`
	IP            string `json:"ip,omitempty"`
	UserAgent     string `json:"user_agent,omitempty"`
}

// SavedLoginLog for persisting login logs
type SavedLoginLog struct {
	Time      string `json:"time"`
	Username  string `json:"username"`
	IP        string `json:"ip"`
	UserAgent string `json:"user_agent"`
	Success   bool   `json:"success"`
}

// AuditLog represents an operation log entry
type AuditLog struct {
	Time      string `json:"time"`
	Action    string `json:"action"`
	Target    string `json:"target"`
	Detail    string `json:"detail"`
	User      string `json:"user"`
	IP        string `json:"ip,omitempty"`
	UserAgent string `json:"user_agent,omitempty"`
	Success   *bool  `json:"success,omitempty"`
	Error     string `json:"error,omitempty"`
}

type VMReadinessCheck struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
}

// Container represents an LXC container configuration
type Container struct {
	ID                            int                    `json:"id"`
	UUID                          string                 `json:"uuid"`
	Name                          string                 `json:"name"`
	Virtualization                string                 `json:"virtualization,omitempty"`
	LXCName                       string                 `json:"lxc_name,omitempty"`
	KVMName                       string                 `json:"kvm_name,omitempty"`
	DiskImage                     string                 `json:"disk_image,omitempty"`
	StoragePoolID                 string                 `json:"storage_pool_id,omitempty"`
	StoragePath                   string                 `json:"storage_path,omitempty"`
	MACAddress                    string                 `json:"mac_address,omitempty"`
	Template                      string                 `json:"template"`
	VCPU                          float64                `json:"vcpu"`
	RAMMB                         int                    `json:"ram_mb"`
	DiskGB                        int                    `json:"disk_gb"`
	NetworkBWMbps                 int                    `json:"network_bw_mbps"`
	NetworkDownMbps               int                    `json:"network_down_mbps"`
	NetworkUpMbps                 int                    `json:"network_up_mbps"`
	MonthlyTrafficGB              int                    `json:"monthly_traffic_gb"`
	TrafficMode                   string                 `json:"traffic_mode"`   // "total" or "in_out"
	TrafficInGB                   int                    `json:"traffic_in_gb"`  // 0 = unlimited
	TrafficOutGB                  int                    `json:"traffic_out_gb"` // 0 = unlimited
	TrafficUsedRX                 int64                  `json:"traffic_used_rx"`
	TrafficUsedTX                 int64                  `json:"traffic_used_tx"`
	TrafficResetDate              string                 `json:"traffic_reset_date"`
	IOSpeedMBps                   int                    `json:"io_speed_mbps"`
	IOReadMBps                    int                    `json:"io_read_mbps"`
	IOWriteMBps                   int                    `json:"io_write_mbps"`
	Status                        string                 `json:"status"`
	RestoreOnHostBoot             bool                   `json:"restore_on_host_boot,omitempty"`
	IP                            string                 `json:"ip"`
	LANIPv4Mode                   string                 `json:"lan_ipv4_mode,omitempty"`
	LANInterface                  string                 `json:"lan_interface,omitempty"`
	LANIPv4Address                string                 `json:"lan_ipv4_address,omitempty"`
	LANIPv4PrefixLen              int                    `json:"lan_ipv4_prefix_len,omitempty"`
	LANIPv4Gateway                string                 `json:"lan_ipv4_gateway,omitempty"`
	PublicIPv4s                   []PublicIPv4Assignment `json:"public_ipv4s,omitempty"`
	IPv6                          string                 `json:"ipv6"`
	IPv6PrefixLen                 int                    `json:"ipv6_prefix_len"`
	IPv6Interface                 string                 `json:"ipv6_interface"`
	IPv6Addresses                 []IPv6Assignment       `json:"ipv6_addresses,omitempty"`
	VNCPort                       int                    `json:"vnc_port"`
	SSHPort                       int                    `json:"ssh_port"`
	SSHPassword                   string                 `json:"ssh_password"`
	SSHHostKey                    string                 `json:"ssh_host_key,omitempty"`
	PortMappings                  []PortMapping          `json:"port_mappings"`
	PortMappingLimit              int                    `json:"port_mapping_limit"`
	FirewallEnabled               bool                   `json:"firewall_enabled"`
	FirewallDefaultAction         string                 `json:"firewall_default_action"`
	FirewallRules                 []FirewallRule         `json:"firewall_rules"`
	AllowedImageIDs               []string               `json:"allowed_image_ids,omitempty"`
	ImageLimitConfigured          bool                   `json:"image_limit_configured,omitempty"`
	SnapshotLimit                 int                    `json:"snapshot_limit"`
	CreatedAt                     string                 `json:"created_at"`
	ExpiresAt                     string                 `json:"expires_at"`
	SnapshotScheduleEnabled       bool                   `json:"snapshot_schedule_enabled"`
	SnapshotScheduleIntervalHours int                    `json:"snapshot_schedule_interval_hours"`
	SnapshotScheduleTime          string                 `json:"snapshot_schedule_time"`
	SnapshotScheduleLastRun       string                 `json:"snapshot_schedule_last_run"`
	SnapshotScheduleNextRun       string                 `json:"snapshot_schedule_next_run"`
	SnapshotScheduleCreatedBy     string                 `json:"snapshot_schedule_created_by"`
	PolicyBlocked                 bool                   `json:"policy_blocked"`
	PolicyBlockedReason           string                 `json:"policy_blocked_reason,omitempty"`
	PolicyBlockedAt               string                 `json:"policy_blocked_at,omitempty"`
}

const (
	VirtualizationLXC = "lxc"
	VirtualizationKVM = "kvm"

	LANIPv4ModeDHCP   = "dhcp"
	LANIPv4ModeStatic = "static"
)

func NormalizeVirtualization(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case VirtualizationKVM:
		return VirtualizationKVM
	default:
		return VirtualizationLXC
	}
}

func (c *Container) Runtime() string {
	return NormalizeVirtualization(c.Virtualization)
}

func (c *Container) IsKVM() bool {
	return c.Runtime() == VirtualizationKVM
}

func (c *Container) UsesLANDHCP() bool {
	return strings.EqualFold(strings.TrimSpace(c.LANIPv4Mode), LANIPv4ModeDHCP)
}

func (c *Container) UsesLANStaticIPv4() bool {
	return strings.EqualFold(strings.TrimSpace(c.LANIPv4Mode), LANIPv4ModeStatic)
}

func (c *Container) UsesLANIPv4() bool {
	return c.UsesLANDHCP() || c.UsesLANStaticIPv4()
}

func normalizeStoragePools() bool {
	if AppConfig == nil {
		return false
	}
	changed := false
	result := make([]StoragePool, 0, len(AppConfig.StoragePools))
	seen := map[string]bool{}
	defaultSeen := map[string]bool{}
	for _, pool := range AppConfig.StoragePools {
		pool.ID = strings.TrimSpace(pool.ID)
		pool.Name = strings.TrimSpace(pool.Name)
		pool.Path = filepath.Clean(strings.TrimSpace(pool.Path))
		pool.MountPoint = filepath.Clean(strings.TrimSpace(pool.MountPoint))
		if pool.MountPoint == "." {
			pool.MountPoint = ""
		}
		if pool.MountPoint != "" {
			managedPath := managedStoragePoolPath(pool.MountPoint)
			if pool.Path != managedPath {
				pool.Path = managedPath
				changed = true
			}
		}
		if pool.ID == "" {
			pool.ID = storagePoolIDFromName(pool.Name, pool.Path)
			changed = true
		}
		if pool.Name == "" {
			pool.Name = pool.ID
			changed = true
		}
		if pool.Path == "." || !filepath.IsAbs(pool.Path) || seen[pool.ID] {
			changed = true
			continue
		}
		seen[pool.ID] = true
		pool.ContentTypes = normalizeStorageContentTypes(pool.ContentTypes)
		pool.DefaultContents = normalizeStorageContentTypes(pool.DefaultContents)
		allowed := map[string]bool{}
		for _, content := range pool.ContentTypes {
			allowed[content] = true
		}
		defaults := make([]string, 0, len(pool.DefaultContents))
		for _, content := range pool.DefaultContents {
			if !allowed[content] || defaultSeen[content] {
				changed = true
				continue
			}
			defaultSeen[content] = true
			defaults = append(defaults, content)
		}
		pool.DefaultContents = defaults
		if pool.ContentTypes == nil {
			pool.ContentTypes = []string{}
		}
		result = append(result, pool)
	}
	if len(result) != len(AppConfig.StoragePools) {
		changed = true
	}
	AppConfig.StoragePools = result
	return changed
}

func managedStoragePoolPath(mountPoint string) string {
	mountPoint = filepath.Clean(strings.TrimSpace(mountPoint))
	if mountPoint == string(os.PathSeparator) {
		return filepath.Join(string(os.PathSeparator), "var", "lib", "clicd")
	}
	return filepath.Join(mountPoint, "clicd")
}

func storagePoolIDFromName(name, path string) string {
	base := strings.ToLower(strings.TrimSpace(name))
	if base == "" {
		base = filepath.Base(filepath.Clean(path))
	}
	replacer := strings.NewReplacer(" ", "-", "_", "-", ".", "-", "/", "-")
	base = replacer.Replace(base)
	base = strings.Trim(base, "-")
	if base == "" {
		base = "storage"
	}
	return base
}

func normalizeStorageContentTypes(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	valid := map[string]bool{
		StorageContentLXC:       true,
		StorageContentKVM:       true,
		StorageContentImages:    true,
		StorageContentSnapshots: true,
		StorageContentBackups:   true,
	}
	seen := map[string]bool{}
	result := []string{}
	for _, value := range values {
		next := strings.ToLower(strings.TrimSpace(value))
		if !valid[next] || seen[next] {
			continue
		}
		seen[next] = true
		result = append(result, next)
	}
	return result
}

func StoragePoolsForContent(content string) []StoragePool {
	if AppConfig == nil {
		return nil
	}
	content = strings.ToLower(strings.TrimSpace(content))
	result := []StoragePool{}
	for _, pool := range AppConfig.StoragePools {
		if !pool.Enabled || !storagePoolAllows(pool, content) {
			continue
		}
		result = append(result, pool)
	}
	return result
}

func StoragePoolByID(id string) *StoragePool {
	if AppConfig == nil {
		return nil
	}
	id = strings.TrimSpace(id)
	for i := range AppConfig.StoragePools {
		if AppConfig.StoragePools[i].ID == id {
			return &AppConfig.StoragePools[i]
		}
	}
	return nil
}

func StoragePoolAllowsContent(pool StoragePool, content string) bool {
	return storagePoolAllows(pool, strings.ToLower(strings.TrimSpace(content)))
}

func StoragePathForContent(content, fallback string) string {
	if pool := DefaultStoragePoolForContent(content); pool != nil {
		return pool.Path
	}
	return fallback
}

// PreferredStoragePoolForContent returns the configured default without doing
// filesystem probes. Use SelectStoragePoolForContent for new writes.
func PreferredStoragePoolForContent(content string) *StoragePool {
	if AppConfig == nil {
		return nil
	}
	content = strings.ToLower(strings.TrimSpace(content))
	for i := range AppConfig.StoragePools {
		pool := &AppConfig.StoragePools[i]
		if !pool.Enabled || !storagePoolAllows(*pool, content) {
			continue
		}
		for _, item := range pool.DefaultContents {
			if item == content {
				return pool
			}
		}
	}
	for i := range AppConfig.StoragePools {
		pool := &AppConfig.StoragePools[i]
		if pool.Enabled && storagePoolAllows(*pool, content) {
			return pool
		}
	}
	return nil
}

func DefaultStoragePoolForContent(content string) *StoragePool {
	pool, _ := SelectStoragePoolForContent(content, "", 0)
	return pool
}

const storagePoolFreeReserveBytes int64 = 256 * 1024 * 1024

type storagePoolCandidate struct {
	pool      *StoragePool
	freeBytes int64
	isDefault bool
}

// SelectStoragePoolForContent picks a writable mounted pool. The requested or
// configured default pool is preferred while it has enough space; remaining
// pools are tried by available space from largest to smallest.
func SelectStoragePoolForContent(content, requestedPoolID string, requiredBytes int64) (*StoragePool, error) {
	if AppConfig == nil {
		return nil, fmt.Errorf("storage configuration is not loaded")
	}
	content = strings.ToLower(strings.TrimSpace(content))
	requestedPoolID = strings.TrimSpace(requestedPoolID)
	if requiredBytes < 0 {
		requiredBytes = 0
	}
	requiredFree := requiredBytes + storagePoolFreeReserveBytes
	candidates := make([]storagePoolCandidate, 0, len(AppConfig.StoragePools))
	configured := 0
	for i := range AppConfig.StoragePools {
		pool := &AppConfig.StoragePools[i]
		if !pool.Enabled || !storagePoolAllows(*pool, content) {
			continue
		}
		configured++
		freeBytes, available := probeStoragePoolFreeBytes(*pool)
		if !available {
			continue
		}
		candidate := storagePoolCandidate{pool: pool, freeBytes: freeBytes}
		for _, item := range pool.DefaultContents {
			if item == content {
				candidate.isDefault = true
				break
			}
		}
		candidates = append(candidates, candidate)
	}
	if configured == 0 {
		return nil, fmt.Errorf("no storage disk is enabled for %s", storageContentLabel(content))
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("all storage disks enabled for %s are unavailable or unmounted", storageContentLabel(content))
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].freeBytes > candidates[j].freeBytes
	})
	preferred := func(match func(storagePoolCandidate) bool) *StoragePool {
		for _, candidate := range candidates {
			if match(candidate) && candidate.freeBytes >= requiredFree {
				return candidate.pool
			}
		}
		return nil
	}
	if requestedPoolID != "" {
		if pool := preferred(func(candidate storagePoolCandidate) bool { return candidate.pool.ID == requestedPoolID }); pool != nil {
			return pool, nil
		}
	}
	if pool := preferred(func(candidate storagePoolCandidate) bool { return candidate.isDefault }); pool != nil {
		return pool, nil
	}
	if pool := preferred(func(storagePoolCandidate) bool { return true }); pool != nil {
		return pool, nil
	}
	return nil, fmt.Errorf("storage disks enabled for %s do not have enough free space", storageContentLabel(content))
}

var probeStoragePoolFreeBytes = storagePoolFreeBytes

func storagePoolFreeBytes(pool StoragePool) (int64, bool) {
	if strings.TrimSpace(pool.Path) == "" {
		return 0, false
	}
	if _, err := os.Stat(pool.Path); err != nil {
		if !os.IsNotExist(err) || filepath.Clean(pool.MountPoint) != string(os.PathSeparator) {
			return 0, false
		}
		if err := os.MkdirAll(pool.Path, 0755); err != nil {
			return 0, false
		}
	}
	if mountPoint := strings.TrimSpace(pool.MountPoint); mountPoint != "" {
		out, err := exec.Command("findmnt", "-n", "-o", "TARGET", "--target", pool.Path).Output()
		if err != nil || filepath.Clean(strings.TrimSpace(string(out))) != filepath.Clean(mountPoint) {
			return 0, false
		}
	}
	out, err := exec.Command("df", "-B1", "-P", pool.Path).Output()
	if err != nil {
		return 0, false
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 2 {
		return 0, false
	}
	fields := strings.Fields(lines[len(lines)-1])
	if len(fields) < 4 {
		return 0, false
	}
	freeBytes, err := strconv.ParseInt(fields[3], 10, 64)
	return freeBytes, err == nil
}

func storageContentLabel(content string) string {
	switch content {
	case StorageContentLXC:
		return "LXC containers"
	case StorageContentKVM:
		return "KVM disks"
	case StorageContentImages:
		return "image cache"
	case StorageContentSnapshots:
		return "snapshots"
	case StorageContentBackups:
		return "backups"
	default:
		return content
	}
}

func storagePoolAllows(pool StoragePool, content string) bool {
	for _, item := range pool.ContentTypes {
		if item == content {
			return true
		}
	}
	return false
}

func (c *Container) NormalizeNetworkAssignments() bool {
	changed := false
	lanMode := strings.ToLower(strings.TrimSpace(c.LANIPv4Mode))
	if lanMode != "" && lanMode != LANIPv4ModeDHCP && lanMode != LANIPv4ModeStatic {
		lanMode = ""
	}
	if c.LANIPv4Mode != lanMode {
		c.LANIPv4Mode = lanMode
		changed = true
	}
	lanInterface := strings.TrimSpace(c.LANInterface)
	if c.LANInterface != lanInterface {
		c.LANInterface = lanInterface
		changed = true
	}
	lanAddress := strings.TrimSpace(c.LANIPv4Address)
	if c.LANIPv4Address != lanAddress {
		c.LANIPv4Address = lanAddress
		changed = true
	}
	lanGateway := strings.TrimSpace(c.LANIPv4Gateway)
	if c.LANIPv4Gateway != lanGateway {
		c.LANIPv4Gateway = lanGateway
		changed = true
	}
	if c.LANIPv4Mode == LANIPv4ModeDHCP {
		if c.LANIPv4Address != "" {
			c.LANIPv4Address = ""
			changed = true
		}
	} else if c.LANIPv4Mode != LANIPv4ModeStatic {
		if c.LANIPv4Address != "" || c.LANIPv4PrefixLen != 0 || c.LANIPv4Gateway != "" {
			c.LANIPv4Address = ""
			c.LANIPv4PrefixLen = 0
			c.LANIPv4Gateway = ""
			changed = true
		}
	}
	seenIPv4 := map[string]bool{}
	filteredIPv4 := make([]PublicIPv4Assignment, 0, len(c.PublicIPv4s))
	for _, item := range c.PublicIPv4s {
		item.Address = strings.TrimSpace(item.Address)
		item.Interface = strings.TrimSpace(item.Interface)
		item.Gateway = strings.TrimSpace(item.Gateway)
		if item.Address == "" || seenIPv4[item.Address] {
			if item.Address != "" {
				changed = true
			}
			continue
		}
		seenIPv4[item.Address] = true
		filteredIPv4 = append(filteredIPv4, item)
	}
	if len(filteredIPv4) != len(c.PublicIPv4s) {
		changed = true
	}
	c.PublicIPv4s = filteredIPv4

	seenIPv6 := map[string]bool{}
	filteredIPv6 := make([]IPv6Assignment, 0, len(c.IPv6Addresses)+1)
	for _, item := range c.IPv6Addresses {
		item.Address = strings.TrimSpace(item.Address)
		item.Interface = strings.TrimSpace(item.Interface)
		if item.Address == "" || seenIPv6[item.Address] {
			if item.Address != "" {
				changed = true
			}
			continue
		}
		seenIPv6[item.Address] = true
		filteredIPv6 = append(filteredIPv6, item)
	}
	if strings.TrimSpace(c.IPv6) != "" && !seenIPv6[c.IPv6] {
		filteredIPv6 = append([]IPv6Assignment{{
			Address:   c.IPv6,
			PrefixLen: c.IPv6PrefixLen,
			Interface: c.IPv6Interface,
		}}, filteredIPv6...)
		changed = true
	}
	if len(filteredIPv6) != len(c.IPv6Addresses) {
		changed = true
	}
	c.IPv6Addresses = filteredIPv6
	if len(c.IPv6Addresses) > 0 {
		first := c.IPv6Addresses[0]
		if c.IPv6 != first.Address || c.IPv6PrefixLen != first.PrefixLen || c.IPv6Interface != first.Interface {
			c.IPv6 = first.Address
			c.IPv6PrefixLen = first.PrefixLen
			c.IPv6Interface = first.Interface
			changed = true
		}
	} else if c.IPv6 != "" || c.IPv6PrefixLen != 0 || c.IPv6Interface != "" {
		c.IPv6 = ""
		c.IPv6PrefixLen = 0
		c.IPv6Interface = ""
		changed = true
	}
	return changed
}

func (c *Container) PublicIPv4Addresses() []string {
	values := make([]string, 0, len(c.PublicIPv4s))
	for _, item := range c.PublicIPv4s {
		if item.Address != "" {
			values = append(values, item.Address)
		}
	}
	return values
}

func (c *Container) PrimaryPublicIPv4() string {
	if len(c.PublicIPv4s) == 0 {
		return ""
	}
	return c.PublicIPv4s[0].Address
}

func (c *Container) IPv6AddressStrings() []string {
	values := make([]string, 0, len(c.IPv6Addresses))
	for _, item := range c.IPv6Addresses {
		if item.Address != "" {
			values = append(values, item.Address)
		}
	}
	if len(values) == 0 && c.IPv6 != "" {
		values = append(values, c.IPv6)
	}
	return values
}

// LxcName returns the internal LXC container name (ct-{id})
func (c *Container) LxcName() string {
	if c.LXCName != "" {
		return c.LXCName
	}
	return fmt.Sprintf("ct-%d", c.ID)
}

// VirshName returns the internal libvirt domain name for KVM instances.
func (c *Container) VirshName() string {
	if c.KVMName != "" {
		return c.KVMName
	}
	return fmt.Sprintf("vm-%d", c.ID)
}

// SubUser represents a sub-user with access to specific containers
type ApiKeyConfig struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	KeyHash        string   `json:"key_hash"`
	Prefix         string   `json:"prefix"`
	IPWhitelist    string   `json:"ip_whitelist"`
	CreatedAt      string   `json:"created_at"`
	LastUsed       string   `json:"last_used"`
	Scopes         []string `json:"scopes,omitempty"`
	ExpiresAt      string   `json:"expires_at,omitempty"`
	Disabled       bool     `json:"disabled,omitempty"`
	ContainerUUIDs []string `json:"container_uuids,omitempty"`
	LastUsedIP     string   `json:"last_used_ip,omitempty"`
}

// DeleteApiKey removes an API key by ID
func DeleteApiKey(id string) {
	filtered := make([]ApiKeyConfig, 0, len(AppConfig.ApiKeys))
	for _, k := range AppConfig.ApiKeys {
		if k.ID != id {
			filtered = append(filtered, k)
		}
	}
	AppConfig.ApiKeys = filtered
	SaveConfig()
}

type SubUser struct {
	ID                   string   `json:"id"`
	Username             string   `json:"username"`
	Password             string   `json:"password,omitempty"`
	PassHash             string   `json:"pass_hash"`
	ContainerNames       []string `json:"container_names"`
	ContainerUUIDs       []string `json:"container_uuids,omitempty"`
	AllowedImageIDs      []string `json:"allowed_image_ids,omitempty"`
	ImageLimitConfigured bool     `json:"image_limit_configured,omitempty"`
	Token                string   `json:"-"`
	AccessCode           string   `json:"access_code"`
	CreatedAt            string   `json:"created_at"`
	TokenVersion         int      `json:"token_version"`
}

type Snapshot struct {
	ID            string `json:"id"`
	ContainerID   int    `json:"container_id"`
	ContainerName string `json:"container_name"`
	LXCName       string `json:"lxc_name"`
	CreatedAt     string `json:"created_at"`
	CreatedBy     string `json:"created_by"`
	Scheduled     bool   `json:"scheduled"`
	Path          string `json:"path"`
	SizeBytes     int64  `json:"size_bytes"`
}

const (
	SSLModeDisabled    = "disabled"
	SSLModeLetsEncrypt = "letsencrypt"
	SSLModeSelfSigned  = "self_signed"
	SSLModeUploaded    = "uploaded"
)

type SSLConfig struct {
	Enabled      bool   `json:"enabled"`
	Mode         string `json:"mode"`
	Target       string `json:"target"`
	Email        string `json:"email,omitempty"`
	CertPath     string `json:"cert_path,omitempty"`
	KeyPath      string `json:"key_path,omitempty"`
	LastIssuedAt string `json:"last_issued_at,omitempty"`
	LastError    string `json:"last_error,omitempty"`
}

const (
	StorageContentLXC       = "lxc"
	StorageContentKVM       = "kvm"
	StorageContentImages    = "images"
	StorageContentSnapshots = "snapshots"
	StorageContentBackups   = "backups"
)

type StoragePool struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Path            string   `json:"path"`
	MountPoint      string   `json:"mount_point,omitempty"`
	ContentTypes    []string `json:"content_types"`
	DefaultContents []string `json:"default_contents,omitempty"`
	Enabled         bool     `json:"enabled"`
}

func defaultPrimaryStoragePool() StoragePool {
	contents := []string{
		StorageContentLXC,
		StorageContentKVM,
		StorageContentImages,
		StorageContentSnapshots,
		StorageContentBackups,
	}
	return StoragePool{
		ID:              "disk-root",
		Name:            "system (/)",
		Path:            "/var/lib/clicd",
		MountPoint:      "/",
		ContentTypes:    append([]string(nil), contents...),
		DefaultContents: append([]string(nil), contents...),
		Enabled:         true,
	}
}

// ClicdConfig is the main configuration structure
type ClicdConfig struct {
	AdminUser            string                 `json:"admin_user"`
	AdminPassHash        string                 `json:"admin_pass_hash"`
	JWTSecret            string                 `json:"jwt_secret"`
	Port                 int                    `json:"port"`
	DataDir              string                 `json:"data_dir"`
	Containers           []Container            `json:"containers"`
	NextContainerID      int                    `json:"next_container_id"`
	NextVNCPort          int                    `json:"next_vnc_port"`
	NextSSHPort          int                    `json:"next_ssh_port"`
	NATPortStart         int                    `json:"nat_port_start"`
	NATPortEnd           int                    `json:"nat_port_end"`
	SetupComplete        bool                   `json:"setup_complete"`
	SubUsers             []SubUser              `json:"sub_users"`
	ApiKeys              []ApiKeyConfig         `json:"api_keys"`
	AuditLogs            []AuditLog             `json:"audit_logs"`
	Tasks                []SavedTask            `json:"tasks"`
	LoginLogs            []SavedLoginLog        `json:"login_logs"`
	EnabledImages        []string               `json:"enabled_images"`
	Snapshots            []Snapshot             `json:"snapshots"`
	PublicIPv4Pool       []PublicIPv4Assignment `json:"public_ipv4_pool"`
	PublicIPv6Prefixes   []PublicIPv6Prefix     `json:"public_ipv6_prefixes"`
	WebSSHAllowedOrigins []string               `json:"webssh_allowed_origins"`
	SecurityAutoShutdown bool                   `json:"security_auto_shutdown"`
	TaskConcurrency      int                    `json:"task_concurrency"`
	Language             string                 `json:"language"`
	SSL                  SSLConfig              `json:"ssl"`
	SSLCertificates      map[string]SSLConfig   `json:"ssl_certificates"`
	StoragePools         []StoragePool          `json:"storage_pools"`
}

var configPath string
var AppConfig *ClicdConfig
var allocationMu sync.Mutex

const DefaultSnapshotLimit = 3

const (
	DefaultTaskConcurrency = 2
	MaxTaskConcurrency     = 16
)

const (
	DefaultNATPortStart = 20000
	DefaultNATPortEnd   = 65535
)

func getConfigPath() string {
	if configPath != "" {
		return configPath
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = "/root"
	}
	return filepath.Join(home, ".clicd", "config.json")
}

func SetConfigPath(path string) {
	configPath = path
}

func getDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "/root"
	}
	return filepath.Join(home, ".clicd")
}

func generateRandomString(length int) string {
	b := make([]byte, length)
	rand.Read(b)
	return hex.EncodeToString(b)[:length]
}

func generateUUIDString() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return generateRandomString(32)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// NewContainerUUID returns a UUID that is unique within the current config.
func NewContainerUUID() string {
	for {
		uuid := generateUUIDString()
		if FindContainerByUUID(uuid) == nil {
			return uuid
		}
	}
}

// InitConfig initializes or loads the configuration
func InitConfig() (*ClicdConfig, error) {
	cfgPath := getConfigPath()
	dataDir := getDataDir()

	if err := os.MkdirAll(filepath.Dir(cfgPath), 0700); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %v", err)
	}
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %v", err)
	}
	if err := openConfigDB(); err != nil {
		return nil, err
	}

	cfg, ok, err := loadConfigFromDB()
	if err != nil {
		return nil, err
	}
	if ok {
		AppConfig = cfg
		changed := normalizeConfigDefaults(dataDir)
		if migrateLoadedConfig() {
			changed = true
		}
		if changed {
			if err := SaveConfig(); err != nil {
				return nil, err
			}
		}
		return AppConfig, nil
	}

	legacy, ok, err := loadLegacyJSONConfig(cfgPath)
	if err != nil {
		return nil, err
	}
	if ok {
		AppConfig = legacy
		normalizeConfigDefaults(dataDir)
		migrateLoadedConfig()
		// Always save legacy JSON data into SQLite.
		if err := SaveConfig(); err != nil {
			return nil, err
		}
		return AppConfig, nil
	}

	adminUser := "admin"
	adminPass := generateRandomString(16)
	jwtSecret := generateRandomString(32)
	hash, err := bcrypt.GenerateFromPassword([]byte(adminPass), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %v", err)
	}

	AppConfig = &ClicdConfig{
		AdminUser:            adminUser,
		AdminPassHash:        string(hash),
		JWTSecret:            jwtSecret,
		Port:                 8999,
		DataDir:              dataDir,
		Containers:           []Container{},
		NextContainerID:      1,
		NextVNCPort:          5900,
		NextSSHPort:          22000,
		NATPortStart:         DefaultNATPortStart,
		NATPortEnd:           DefaultNATPortEnd,
		SetupComplete:        false,
		SubUsers:             []SubUser{},
		AuditLogs:            []AuditLog{},
		Tasks:                []SavedTask{},
		LoginLogs:            []SavedLoginLog{},
		Snapshots:            []Snapshot{},
		PublicIPv4Pool:       []PublicIPv4Assignment{},
		PublicIPv6Prefixes:   []PublicIPv6Prefix{},
		WebSSHAllowedOrigins: []string{},
		TaskConcurrency:      DefaultTaskConcurrency,
		StoragePools:         []StoragePool{defaultPrimaryStoragePool()},
	}

	if err := SaveConfig(); err != nil {
		return nil, err
	}

	fmt.Println("\n========================================")
	fmt.Println("  CLICD - LXC Container Manager")
	fmt.Println("========================================")
	fmt.Printf("  Username: %s\n", adminUser)
	fmt.Printf("  Password: %s\n", adminPass)
	fmt.Println("========================================")
	fmt.Println("  Please save these credentials!")
	fmt.Println("  Web Interface: http://0.0.0.0:8999")
	fmt.Println("========================================")
	fmt.Println()

	return AppConfig, nil
}

func normalizeConfigDefaults(dataDir string) bool {
	changed := false
	if AppConfig.Port == 0 {
		AppConfig.Port = 8999
		changed = true
	}
	if AppConfig.NextVNCPort == 0 {
		AppConfig.NextVNCPort = 5900
		changed = true
	}
	if AppConfig.NextSSHPort == 0 {
		AppConfig.NextSSHPort = 22000
		changed = true
	}
	if normalizeNATPortRangeDefaults() {
		changed = true
	}
	if AppConfig.NextContainerID == 0 {
		AppConfig.NextContainerID = 1
		changed = true
	}
	if normalized := NormalizeTaskConcurrency(AppConfig.TaskConcurrency); AppConfig.TaskConcurrency != normalized {
		AppConfig.TaskConcurrency = normalized
		changed = true
	}
	if AppConfig.DataDir == "" {
		AppConfig.DataDir = dataDir
		changed = true
	}
	if AppConfig.Containers == nil {
		AppConfig.Containers = make([]Container, 0)
		changed = true
	}
	if AppConfig.Snapshots == nil {
		AppConfig.Snapshots = make([]Snapshot, 0)
		changed = true
	}
	if AppConfig.PublicIPv4Pool == nil {
		AppConfig.PublicIPv4Pool = make([]PublicIPv4Assignment, 0)
		changed = true
	}
	if AppConfig.PublicIPv6Prefixes == nil {
		AppConfig.PublicIPv6Prefixes = make([]PublicIPv6Prefix, 0)
		changed = true
	}
	if AppConfig.WebSSHAllowedOrigins == nil {
		AppConfig.WebSSHAllowedOrigins = make([]string, 0)
		changed = true
	} else if normalized, err := NormalizeAllowedOrigins(AppConfig.WebSSHAllowedOrigins); err == nil && strings.Join(normalized, "\n") != strings.Join(AppConfig.WebSSHAllowedOrigins, "\n") {
		AppConfig.WebSSHAllowedOrigins = normalized
		changed = true
	}
	if len(AppConfig.StoragePools) == 0 {
		AppConfig.StoragePools = []StoragePool{defaultPrimaryStoragePool()}
		changed = true
	}
	if normalizeStoragePools() {
		changed = true
	}
	if AppConfig.SubUsers == nil {
		AppConfig.SubUsers = make([]SubUser, 0)
		changed = true
	}
	if AppConfig.ApiKeys == nil {
		AppConfig.ApiKeys = make([]ApiKeyConfig, 0)
		changed = true
	} else {
		for i := range AppConfig.ApiKeys {
			if len(AppConfig.ApiKeys[i].Scopes) == 0 {
				AppConfig.ApiKeys[i].Scopes = []string{"*"}
				changed = true
			}
		}
	}
	if AppConfig.AuditLogs == nil {
		AppConfig.AuditLogs = make([]AuditLog, 0)
		changed = true
	}
	if AppConfig.Tasks == nil {
		AppConfig.Tasks = make([]SavedTask, 0)
		changed = true
	}
	if AppConfig.LoginLogs == nil {
		AppConfig.LoginLogs = make([]SavedLoginLog, 0)
		changed = true
	}
	if AppConfig.EnabledImages == nil {
		AppConfig.EnabledImages = make([]string, 0)
		changed = true
	}
	if AppConfig.Language == "" {
		AppConfig.Language = "zh"
		changed = true
	}
	if AppConfig.Language != "zh" && AppConfig.Language != "en" {
		AppConfig.Language = "zh"
		changed = true
	}
	if normalizeSSLDefaults() {
		changed = true
	}
	return changed
}

func NormalizeTaskConcurrency(value int) int {
	if value <= 0 {
		return DefaultTaskConcurrency
	}
	if value > MaxTaskConcurrency {
		return MaxTaskConcurrency
	}
	return value
}

func NormalizeLanguage(language string) string {
	switch strings.ToLower(strings.TrimSpace(language)) {
	case "en", "en-us", "en_us", "english":
		return "en"
	default:
		return "zh"
	}
}

func normalizeSSLDefaults() bool {
	changed := false
	previousMode := AppConfig.SSL.Mode
	AppConfig.SSL.Mode = NormalizeSSLMode(AppConfig.SSL.Mode)
	if AppConfig.SSL.Mode != previousMode {
		changed = true
	}
	if AppConfig.SSL.Mode == SSLModeDisabled {
		if AppConfig.SSL.Enabled {
			changed = true
		}
		AppConfig.SSL.Enabled = false
	}
	if AppConfig.SSLCertificates == nil {
		AppConfig.SSLCertificates = map[string]SSLConfig{}
		changed = true
	}
	for mode, cert := range AppConfig.SSLCertificates {
		cert.Mode = NormalizeSSLMode(cert.Mode)
		if cert.Mode == SSLModeDisabled {
			delete(AppConfig.SSLCertificates, mode)
			changed = true
			continue
		}
		if AppConfig.SSLCertificates[cert.Mode] != cert {
			changed = true
		}
		AppConfig.SSLCertificates[cert.Mode] = cert
		if mode != cert.Mode {
			delete(AppConfig.SSLCertificates, mode)
			changed = true
		}
	}
	if AppConfig.SSL.Mode != SSLModeDisabled && AppConfig.SSL.CertPath != "" && AppConfig.SSL.KeyPath != "" {
		cert := AppConfig.SSL
		cert.Enabled = false
		if AppConfig.SSLCertificates[cert.Mode] != cert {
			changed = true
		}
		AppConfig.SSLCertificates[cert.Mode] = cert
	}
	return changed
}

func NormalizeSSLMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case SSLModeLetsEncrypt:
		return SSLModeLetsEncrypt
	case SSLModeSelfSigned:
		return SSLModeSelfSigned
	case SSLModeUploaded:
		return SSLModeUploaded
	default:
		return SSLModeDisabled
	}
}

func migrateLoadedConfig() bool {
	changed := ensureContainerUUIDs()
	if ensureContainerVirtualization() {
		changed = true
	}
	if ensureContainerPortMappingLimits() {
		changed = true
	}
	if ensureContainerSnapshotLimits() {
		changed = true
	}
	if ensureContainerNetworkAssignments() {
		changed = true
	}
	if ensureContainerResourceAliases() {
		changed = true
	}
	if ensureContainerSnapshotScheduleDefaults() {
		changed = true
	}
	if migrateSubUsers() {
		changed = true
	}
	if removeLegacyVNCMappings() {
		changed = true
	}
	return changed
}

func ensureContainerVirtualization() bool {
	changed := false
	for i := range AppConfig.Containers {
		next := NormalizeVirtualization(AppConfig.Containers[i].Virtualization)
		if AppConfig.Containers[i].Virtualization != next {
			AppConfig.Containers[i].Virtualization = next
			changed = true
		}
	}
	return changed
}

func ensureContainerSnapshotScheduleDefaults() bool {
	changed := false
	for i := range AppConfig.Containers {
		if AppConfig.Containers[i].SnapshotScheduleEnabled && AppConfig.Containers[i].SnapshotScheduleIntervalHours < 24 {
			AppConfig.Containers[i].SnapshotScheduleIntervalHours = 24
			changed = true
		}
		if AppConfig.Containers[i].SnapshotScheduleEnabled && AppConfig.Containers[i].SnapshotScheduleTime == "" {
			AppConfig.Containers[i].SnapshotScheduleTime = "03:00"
			changed = true
		}
	}
	return changed
}

func ensureContainerUUIDs() bool {
	changed := false
	used := make(map[string]bool)
	for i := range AppConfig.Containers {
		uuid := AppConfig.Containers[i].UUID
		if uuid == "" || used[uuid] {
			for {
				uuid = generateUUIDString()
				if !used[uuid] {
					break
				}
			}
			AppConfig.Containers[i].UUID = uuid
			changed = true
		}
		used[uuid] = true
	}
	return changed
}

func ensureContainerPortMappingLimits() bool {
	changed := false
	for i := range AppConfig.Containers {
		if AppConfig.Containers[i].PortMappingLimit < 0 {
			limit := len(AppConfig.Containers[i].PortMappings)
			if limit < 2 {
				limit = 2
			}
			AppConfig.Containers[i].PortMappingLimit = limit
			changed = true
		} else if AppConfig.Containers[i].PortMappingLimit == 0 && len(AppConfig.Containers[i].PortMappings) > 0 {
			AppConfig.Containers[i].PortMappingLimit = len(AppConfig.Containers[i].PortMappings)
			changed = true
		}
	}
	return changed
}

func ensureContainerSnapshotLimits() bool {
	changed := false
	for i := range AppConfig.Containers {
		if AppConfig.Containers[i].SnapshotLimit <= 0 {
			AppConfig.Containers[i].SnapshotLimit = DefaultSnapshotLimit
			changed = true
		}
	}
	return changed
}

func ensureContainerNetworkAssignments() bool {
	changed := false
	for i := range AppConfig.Containers {
		if AppConfig.Containers[i].NormalizeNetworkAssignments() {
			changed = true
		}
	}
	return changed
}

func ensureContainerResourceAliases() bool {
	changed := false
	for i := range AppConfig.Containers {
		if NormalizeContainerResourceAliases(&AppConfig.Containers[i]) {
			changed = true
		}
	}
	return changed
}

func NormalizeContainerResourceAliases(c *Container) bool {
	if c == nil {
		return false
	}
	changed := false
	if c.NetworkBWMbps < 0 {
		c.NetworkBWMbps = 0
		changed = true
	}
	if c.NetworkDownMbps < 0 {
		c.NetworkDownMbps = 0
		changed = true
	}
	if c.NetworkUpMbps < 0 {
		c.NetworkUpMbps = 0
		changed = true
	}
	if c.NetworkDownMbps == 0 && c.NetworkUpMbps == 0 && c.NetworkBWMbps > 0 {
		c.NetworkDownMbps = c.NetworkBWMbps
		c.NetworkUpMbps = c.NetworkBWMbps
		changed = true
	}
	nextNetworkBW := LegacySymmetricLimit(c.NetworkDownMbps, c.NetworkUpMbps)
	if c.NetworkBWMbps != nextNetworkBW {
		c.NetworkBWMbps = nextNetworkBW
		changed = true
	}

	if c.IOSpeedMBps < 0 {
		c.IOSpeedMBps = 0
		changed = true
	}
	if c.IOReadMBps < 0 {
		c.IOReadMBps = 0
		changed = true
	}
	if c.IOWriteMBps < 0 {
		c.IOWriteMBps = 0
		changed = true
	}
	if c.IOReadMBps == 0 && c.IOWriteMBps == 0 && c.IOSpeedMBps > 0 {
		c.IOReadMBps = c.IOSpeedMBps
		c.IOWriteMBps = c.IOSpeedMBps
		changed = true
	}
	nextIO := LegacySymmetricLimit(c.IOReadMBps, c.IOWriteMBps)
	if c.IOSpeedMBps != nextIO {
		c.IOSpeedMBps = nextIO
		changed = true
	}
	return changed
}

func LegacySymmetricLimit(a, b int) int {
	if a < 0 {
		a = 0
	}
	if b < 0 {
		b = 0
	}
	if a == b {
		return a
	}
	if a == 0 {
		return b
	}
	if b == 0 {
		return a
	}
	if a < b {
		return a
	}
	return b
}

func migrateSubUsers() bool {
	changed := false
	for i := range AppConfig.SubUsers {
		su := &AppConfig.SubUsers[i]
		if su.PassHash == "" && su.Password != "" {
			if hash, err := bcrypt.GenerateFromPassword([]byte(su.Password), bcrypt.DefaultCost); err == nil {
				su.PassHash = string(hash)
				changed = true
			}
		}
		if su.Token != "" {
			su.Token = ""
			changed = true
		}
		if len(su.ContainerUUIDs) == 0 && len(su.ContainerNames) > 0 {
			for _, name := range su.ContainerNames {
				if c := FindContainerByName(name); c != nil && c.UUID != "" {
					su.ContainerUUIDs = appendUniqueString(su.ContainerUUIDs, c.UUID)
				}
			}
			if len(su.ContainerUUIDs) > 0 {
				changed = true
			}
		}
	}
	return changed
}

func appendUniqueString(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func NormalizeSnapshotLimit(limit int) int {
	if limit <= 0 {
		return DefaultSnapshotLimit
	}
	return limit
}

func ContainerSnapshotLimit(c *Container) int {
	if c == nil {
		return DefaultSnapshotLimit
	}
	return NormalizeSnapshotLimit(c.SnapshotLimit)
}

func removeLegacyVNCMappings() bool {
	changed := false
	for i := range AppConfig.Containers {
		mappings := AppConfig.Containers[i].PortMappings
		if len(mappings) == 0 {
			continue
		}

		filtered := mappings[:0]
		for _, pm := range mappings {
			isLegacyVNC := strings.EqualFold(pm.Description, "VNC") || pm.ContainerPort == 5901
			if isLegacyVNC {
				changed = true
				continue
			}
			filtered = append(filtered, pm)
		}
		AppConfig.Containers[i].PortMappings = filtered
	}
	return changed
}

// SaveConfig saves configuration to disk
func SaveConfig() error {
	return saveConfigToDB()
}

// AddContainer adds a container to the config
func AddContainer(c Container) {
	allocationMu.Lock()
	defer allocationMu.Unlock()
	if c.UUID == "" {
		c.UUID = NewContainerUUID()
	}
	c.Virtualization = NormalizeVirtualization(c.Virtualization)
	NormalizeContainerResourceAliases(&c)
	AppConfig.Containers = append(AppConfig.Containers, c)
	SaveConfig()
}

// AllocateContainerID allocates a new container ID
func AllocateContainerID() int {
	allocationMu.Lock()
	defer allocationMu.Unlock()
	id := AppConfig.NextContainerID
	AppConfig.NextContainerID++
	SaveConfig()
	return id
}

// RemoveContainer removes a container from config by ID
func RemoveContainer(id int) bool {
	for i, c := range AppConfig.Containers {
		if c.ID == id {
			removeSubUserContainerAccess(c.Name, c.UUID)
			removeContainerSnapshotMetadata(id)
			// Clear snapshot schedule for this container
			clearContainerSnapshotSchedule(&AppConfig.Containers[i])
			AppConfig.Containers = append(AppConfig.Containers[:i], AppConfig.Containers[i+1:]...)
			SaveConfig()
			return true
		}
	}
	return false
}

func clearContainerSnapshotSchedule(c *Container) {
	c.SnapshotScheduleEnabled = false
	c.SnapshotScheduleIntervalHours = 0
	c.SnapshotScheduleTime = ""
	c.SnapshotScheduleLastRun = ""
	c.SnapshotScheduleNextRun = ""
	c.SnapshotScheduleCreatedBy = ""
}

func AddSnapshot(snapshot Snapshot) {
	AppConfig.Snapshots = append(AppConfig.Snapshots, snapshot)
	SaveConfig()
}

func FindSnapshot(id string) *Snapshot {
	for i := range AppConfig.Snapshots {
		if AppConfig.Snapshots[i].ID == id {
			return &AppConfig.Snapshots[i]
		}
	}
	return nil
}

func RemoveSnapshot(id string) bool {
	for i := range AppConfig.Snapshots {
		if AppConfig.Snapshots[i].ID == id {
			AppConfig.Snapshots = append(AppConfig.Snapshots[:i], AppConfig.Snapshots[i+1:]...)
			SaveConfig()
			return true
		}
	}
	return false
}

func ContainerSnapshots(containerID int) []Snapshot {
	result := make([]Snapshot, 0)
	for _, snapshot := range AppConfig.Snapshots {
		if snapshot.ContainerID == containerID {
			result = append(result, snapshot)
		}
	}
	return result
}

func removeContainerSnapshotMetadata(containerID int) {
	filtered := make([]Snapshot, 0, len(AppConfig.Snapshots))
	for _, snapshot := range AppConfig.Snapshots {
		if snapshot.ContainerID != containerID {
			filtered = append(filtered, snapshot)
		}
	}
	AppConfig.Snapshots = filtered
}

func RemoveSubUserContainerAccess(containerName string, containerUUID string) {
	removeSubUserContainerAccess(containerName, containerUUID)
	SaveConfig()
}

func removeSubUserContainerAccess(containerName string, containerUUID string) {
	if containerName == "" && containerUUID == "" || len(AppConfig.SubUsers) == 0 {
		return
	}
	filteredUsers := make([]SubUser, 0, len(AppConfig.SubUsers))
	for _, su := range AppConfig.SubUsers {
		filteredNames := make([]string, 0, len(su.ContainerNames))
		for _, name := range su.ContainerNames {
			if name != containerName {
				filteredNames = append(filteredNames, name)
			}
		}
		filteredUUIDs := make([]string, 0, len(su.ContainerUUIDs))
		for _, uuid := range su.ContainerUUIDs {
			if uuid != containerUUID {
				filteredUUIDs = append(filteredUUIDs, uuid)
			}
		}
		if len(filteredNames) == 0 && len(filteredUUIDs) == 0 {
			continue
		}
		su.ContainerNames = filteredNames
		su.ContainerUUIDs = filteredUUIDs
		filteredUsers = append(filteredUsers, su)
	}
	AppConfig.SubUsers = filteredUsers
}

// FindContainer finds a container by ID
func FindContainer(id int) *Container {
	for i, c := range AppConfig.Containers {
		if c.ID == id {
			return &AppConfig.Containers[i]
		}
	}
	return nil
}

// FindContainerByUUID finds a container by UUID.
func FindContainerByUUID(uuid string) *Container {
	for i, c := range AppConfig.Containers {
		if c.UUID == uuid {
			return &AppConfig.Containers[i]
		}
	}
	return nil
}

// FindContainerByName finds a container by name
func FindContainerByName(name string) *Container {
	for i, c := range AppConfig.Containers {
		if c.Name == name {
			return &AppConfig.Containers[i]
		}
	}
	return nil
}

// FindContainerByIdentifier finds a container by ID, UUID, or name.
func FindContainerByIdentifier(identifier string) *Container {
	if id, err := strconv.Atoi(identifier); err == nil {
		if c := FindContainer(id); c != nil {
			return c
		}
	}
	if c := FindContainerByUUID(identifier); c != nil {
		return c
	}
	return FindContainerByName(identifier)
}

// UpdateContainerStatus updates container status by ID
func UpdateContainerStatus(id int, status string) {
	c := FindContainer(id)
	if c != nil {
		c.Status = status
		SaveConfig()
	}
}

func UpdateContainerStatusAndRestore(id int, status string, restoreOnHostBoot bool) {
	c := FindContainer(id)
	if c != nil {
		c.Status = status
		c.RestoreOnHostBoot = restoreOnHostBoot
		SaveConfig()
	}
}

func SetContainerRestoreOnHostBoot(id int, restore bool) {
	c := FindContainer(id)
	if c != nil {
		c.RestoreOnHostBoot = restore
		SaveConfig()
	}
}

func SetContainerPolicyBlock(id int, blocked bool, reason string) {
	c := FindContainer(id)
	if c == nil {
		return
	}
	c.PolicyBlocked = blocked
	if blocked {
		c.PolicyBlockedReason = reason
		c.PolicyBlockedAt = time.Now().Format("2006-01-02 15:04:05")
	} else {
		c.PolicyBlockedReason = ""
		c.PolicyBlockedAt = ""
	}
	SaveConfig()
}

// UpdateVNC refreshes all container statuses
func UpdateVNC(containers []Container) {
	AppConfig.Containers = containers
	SaveConfig()
}

func NormalizeNATPortRange(start, end int) (int, int, error) {
	if start == 0 && end == 0 {
		return DefaultNATPortStart, DefaultNATPortEnd, nil
	}
	if start == 0 {
		start = DefaultNATPortStart
	}
	if end == 0 {
		end = DefaultNATPortEnd
	}
	if start < 1 || start > 65535 {
		return 0, 0, fmt.Errorf("NAT port start must be 1-65535")
	}
	if end < 1 || end > 65535 {
		return 0, 0, fmt.Errorf("NAT port end must be 1-65535")
	}
	if start > end {
		return 0, 0, fmt.Errorf("NAT port start cannot be greater than end")
	}
	return start, end, nil
}

func NATPortRange() (int, int) {
	if AppConfig == nil {
		return DefaultNATPortStart, DefaultNATPortEnd
	}
	start, end, err := NormalizeNATPortRange(AppConfig.NATPortStart, AppConfig.NATPortEnd)
	if err != nil {
		return DefaultNATPortStart, DefaultNATPortEnd
	}
	return start, end
}

func NATPortCapacity() int {
	start, end := NATPortRange()
	return end - start + 1
}

func NATPortInRange(port int) bool {
	start, end := NATPortRange()
	return port >= start && port <= end
}

func SetNATPortRange(start, end int) error {
	start, end, err := NormalizeNATPortRange(start, end)
	if err != nil {
		return err
	}
	AppConfig.NATPortStart = start
	AppConfig.NATPortEnd = end
	if AppConfig.NextSSHPort < start || AppConfig.NextSSHPort > end {
		AppConfig.NextSSHPort = start
	}
	return SaveConfig()
}

func normalizeNATPortRangeDefaults() bool {
	if AppConfig == nil {
		return false
	}
	start, end, err := NormalizeNATPortRange(AppConfig.NATPortStart, AppConfig.NATPortEnd)
	if err != nil {
		start, end = DefaultNATPortStart, DefaultNATPortEnd
	}
	changed := AppConfig.NATPortStart != start || AppConfig.NATPortEnd != end
	AppConfig.NATPortStart = start
	AppConfig.NATPortEnd = end
	if AppConfig.NextSSHPort < start || AppConfig.NextSSHPort > end {
		AppConfig.NextSSHPort = start
		changed = true
	}
	return changed
}

// AllocateSSHPort allocates a new SSH port, skipping ports already used by any container
func AllocateSSHPort() (int, error) {
	return AllocateSSHPortExcluding(nil)
}

// AllocateSSHPortExcluding allocates a management port while reserving
// user-requested NAT host ports for the container being created.
func AllocateSSHPortExcluding(excluded []int) (int, error) {
	allocationMu.Lock()
	defer allocationMu.Unlock()
	used := collectAllHostPorts()
	for _, port := range excluded {
		if port > 0 {
			used[port] = true
		}
	}
	start, end := NATPortRange()
	port := AppConfig.NextSSHPort
	if port < start || port > end {
		port = start
	}
	capacity := end - start + 1
	for i := 0; i < capacity; i++ {
		candidate := start + ((port - start + i) % capacity)
		if used[candidate] {
			continue
		}
		AppConfig.NextSSHPort = candidate + 1
		if AppConfig.NextSSHPort > end {
			AppConfig.NextSSHPort = start
		}
		SaveConfig()
		return candidate, nil
	}
	return 0, fmt.Errorf("no free NAT4 host port in configured range %d-%d", start, end)
}

// collectAllHostPorts collects all host ports used by any container (LXC + KVM)
func collectAllHostPorts() map[int]bool {
	used := map[int]bool{}
	for _, c := range AppConfig.Containers {
		for _, pm := range c.PortMappings {
			used[pm.HostPort] = true
		}
	}
	return used
}

// IsValidContainerName checks if container name is valid (no duplicate check needed, ID is primary key)
func IsValidContainerName(name string) bool {
	return IsValidContainerNameSyntax(name)
}

// IsValidContainerNameSyntax checks only the container name format.
func IsValidContainerNameSyntax(name string) bool {
	if len(name) == 0 || len(name) > 63 {
		return false
	}
	// Only allow alphanumeric, hyphens, underscores
	for _, c := range name {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

// AddAuditLog adds an audit log entry
func AddAuditLog(action, target, detail, user string) {
	log := AuditLog{
		Time:   time.Now().Format("2006-01-02 15:04:05"),
		Action: action,
		Target: target,
		Detail: detail,
		User:   user,
	}
	AppConfig.AuditLogs = append(AppConfig.AuditLogs, log)
	if len(AppConfig.AuditLogs) > 500 {
		AppConfig.AuditLogs = AppConfig.AuditLogs[len(AppConfig.AuditLogs)-500:]
	}
	SaveConfig()
}

func AddAuditLogFull(action, target, detail, user, ip, userAgent string, success bool, errMsg string) {
	s := success
	log := AuditLog{
		Time:      time.Now().Format("2006-01-02 15:04:05"),
		Action:    action,
		Target:    target,
		Detail:    detail,
		User:      user,
		IP:        ip,
		UserAgent: userAgent,
		Success:   &s,
		Error:     errMsg,
	}
	AppConfig.AuditLogs = append(AppConfig.AuditLogs, log)
	if len(AppConfig.AuditLogs) > 500 {
		AppConfig.AuditLogs = AppConfig.AuditLogs[len(AppConfig.AuditLogs)-500:]
	}
	SaveConfig()
}

// SaveTasks persists the task queue to config
func SaveTasks(tasks []SavedTask) {
	AppConfig.Tasks = tasks
	SaveConfig()
}

// AddLoginLog persists a login log entry
func AddLoginLog(username, ip, userAgent string, success bool) {
	log := SavedLoginLog{
		Time:      time.Now().Format("2006-01-02 15:04:05 MST"),
		Username:  username,
		IP:        ip,
		UserAgent: userAgent,
		Success:   success,
	}
	AppConfig.LoginLogs = append(AppConfig.LoginLogs, log)
	if len(AppConfig.LoginLogs) > 200 {
		AppConfig.LoginLogs = AppConfig.LoginLogs[len(AppConfig.LoginLogs)-200:]
	}
	SaveConfig()
}

// ResetAdminPassword resets the admin password from CLI
func ResetAdminPassword(newPassword string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	AppConfig.AdminPassHash = string(hash)
	return SaveConfig()
}

// CleanStaleContainers removes containers from config if their LXC directory doesn't exist
func CleanStaleContainers() {
	valid := make([]Container, 0)
	changed := false
	for _, c := range AppConfig.Containers {
		if c.IsKVM() {
			if c.DiskImage == "" {
				valid = append(valid, c)
				continue
			}
			if _, err := os.Stat(c.DiskImage); os.IsNotExist(err) {
				fmt.Printf("Cleaning stale KVM config: %s (disk image not found)\n", c.VirshName())
				changed = true
				continue
			}
			valid = append(valid, c)
			continue
		}
		lxcDir := "/var/lib/lxc/" + c.LxcName()
		if _, err := os.Stat(lxcDir); os.IsNotExist(err) {
			fmt.Printf("Cleaning stale container config: %s (LXC dir not found)\n", c.LxcName())
			changed = true
			continue
		}
		valid = append(valid, c)
	}
	if changed {
		AppConfig.Containers = valid
		SaveConfig()
	}
}
