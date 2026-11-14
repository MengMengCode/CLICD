package api

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"clicd/internal/lxc"
)

type HostInfo struct {
	CPU     CpuInfo          `json:"cpu"`
	RAM     MemoryInfo       `json:"ram"`
	Disk    DiskInfo         `json:"disk"`
	Network NetworkInfo      `json:"network"`
	DiskIO  DiskIOInfo       `json:"disk_io"`
	Load    LoadInfo         `json:"load"`
	Runtime HostRuntimeProbe `json:"runtime"`
}

type HostProbeReport struct {
	GeneratedAt       string                `json:"generated_at"`
	Hostname          string                `json:"hostname"`
	Kernel            string                `json:"kernel"`
	OS                string                `json:"os"`
	CPU               HostCPUProbe          `json:"cpu"`
	Memory            HostMemoryProbe       `json:"memory"`
	Disks             []HostDiskProbe       `json:"disks"`
	NetworkInterfaces []HostNICProbe        `json:"network_interfaces"`
	PublicIPv4        []string              `json:"public_ipv4"`
	IPv4Addresses     []HostIPProbe         `json:"ipv4_addresses"`
	IPv4Prefixes      []HostIPv4PrefixProbe `json:"ipv4_prefixes"`
	IPv6Addresses     []HostIPProbe         `json:"ipv6_addresses"`
	IPv6Prefixes      []lxc.IPv6PrefixInfo  `json:"ipv6_prefixes"`
	Gateways          []HostGatewayProbe    `json:"gateways"`
	GPUs              []HostGPUProbe        `json:"gpus"`
	Runtime           HostRuntimeProbe      `json:"runtime"`
	System            HostSystemProbe       `json:"system"`
	Environment       []HostEnvCheck        `json:"environment"`
}

type HostCPUProbe struct {
	Model             string   `json:"model"`
	Cores             int      `json:"cores"`
	Threads           int      `json:"threads"`
	Architecture      string   `json:"architecture"`
	Flags             []string `json:"flags"`
	HasIntegratedGPU  bool     `json:"has_integrated_gpu"`
	Virtualization    bool     `json:"virtualization"`
	VirtualizationKey string   `json:"virtualization_key"`
}

type HostMemoryProbe struct {
	TotalMB int64              `json:"total_mb"`
	UsedMB  int64              `json:"used_mb"`
	FreeMB  int64              `json:"free_mb"`
	Modules []HostMemoryModule `json:"modules"`
}

type HostMemoryModule struct {
	Locator      string `json:"locator"`
	Size         string `json:"size"`
	Type         string `json:"type"`
	Speed        string `json:"speed"`
	Manufacturer string `json:"manufacturer"`
	PartNumber   string `json:"part_number"`
	SerialNumber string `json:"serial_number"`
}

type HostDiskProbe struct {
	Name         string             `json:"name"`
	Path         string             `json:"path"`
	Model        string             `json:"model"`
	Serial       string             `json:"serial"`
	SizeBytes    uint64             `json:"size_bytes"`
	Type         string             `json:"type"`
	Virtual      bool               `json:"virtual"`
	Rotational   bool               `json:"rotational"`
	Mountpoints  []string           `json:"mountpoints"`
	Health       string             `json:"health"`
	HealthDetail string             `json:"health_detail"`
	SMART        HostDiskSMARTProbe `json:"smart"`
}

type HostDiskSMARTProbe struct {
	Available         bool   `json:"available"`
	LifeUsedPercent   *int   `json:"life_used_percent,omitempty"`
	PowerOnHours      int64  `json:"power_on_hours,omitempty"`
	PowerCycleCount   int64  `json:"power_cycle_count,omitempty"`
	ReadDataBytes     uint64 `json:"read_data_bytes,omitempty"`
	WrittenDataBytes  uint64 `json:"written_data_bytes,omitempty"`
	ReadCommands      uint64 `json:"read_commands,omitempty"`
	WriteCommands     uint64 `json:"write_commands,omitempty"`
	WearLevelingCount string `json:"wear_leveling_count,omitempty"`
	EraseCount        string `json:"erase_count,omitempty"`
	MediaErrors       uint64 `json:"media_errors,omitempty"`
}

type HostNICProbe struct {
	Name      string        `json:"name"`
	MAC       string        `json:"mac"`
	State     string        `json:"state"`
	SpeedMbps int           `json:"speed_mbps"`
	Driver    string        `json:"driver"`
	Model     string        `json:"model"`
	IPv4      []HostIPProbe `json:"ipv4"`
	IPv6      []HostIPProbe `json:"ipv6"`
}

type HostIPProbe struct {
	Interface string `json:"interface"`
	Address   string `json:"address"`
	PrefixLen int    `json:"prefix_len"`
	Scope     string `json:"scope"`
	Gateway   string `json:"gateway,omitempty"`
}

type HostIPv4PrefixProbe struct {
	Interface  string `json:"interface"`
	Address    string `json:"address"`
	Prefix     string `json:"prefix"`
	PrefixLen  int    `json:"prefix_len"`
	SubnetMask string `json:"subnet_mask"`
	Gateway    string `json:"gateway"`
	Source     string `json:"source"`
}

type HostGatewayProbe struct {
	Family    string `json:"family"`
	Interface string `json:"interface"`
	Gateway   string `json:"gateway"`
}

type HostGPUProbe struct {
	Name   string `json:"name"`
	Vendor string `json:"vendor"`
	Driver string `json:"driver"`
	Type   string `json:"type"`
}

type HostRuntimeProbe struct {
	LXCAvailable         bool   `json:"lxc_available"`
	KVMAvailable         bool   `json:"kvm_available"`
	DevKVM               bool   `json:"dev_kvm"`
	NestedVirtualization bool   `json:"nested_virtualization"`
	NestedDetail         string `json:"nested_detail"`
	SupportMode          string `json:"support_mode"`
}

type HostSystemProbe struct {
	UptimeSeconds int64  `json:"uptime_seconds"`
	UptimeText    string `json:"uptime_text"`
	ProcessCount  int    `json:"process_count"`
}

type HostEnvCheck struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	OK       bool   `json:"ok"`
	Required bool   `json:"required"`
	Detail   string `json:"detail"`
}

type LoadInfo struct {
	Load1  float64 `json:"load1"`
	Load5  float64 `json:"load5"`
	Load15 float64 `json:"load15"`
}

type CpuInfo struct {
	Cores int     `json:"cores"`
	Usage float64 `json:"usage_pct"`
}

type MemoryInfo struct {
	TotalMB int64 `json:"total_mb"`
	UsedMB  int64 `json:"used_mb"`
	FreeMB  int64 `json:"free_mb"`
}

type DiskInfo struct {
	TotalGB float64 `json:"total_gb"`
	UsedGB  float64 `json:"used_gb"`
	FreeGB  float64 `json:"free_gb"`
}

type NetworkInfo struct {
	RXBytes             uint64               `json:"rx_bytes"`
	TXBytes             uint64               `json:"tx_bytes"`
	RXBps               float64              `json:"rx_bps"`
	TXBps               float64              `json:"tx_bps"`
	PublicIPv4          string               `json:"public_ipv4"`
	PublicIPv4Interface string               `json:"public_ipv4_interface"`
	PublicIPv4Addresses []lxc.PublicIPInfo   `json:"public_ipv4_addresses"`
	PublicIPv6          string               `json:"public_ipv6"`
	PublicIPv6Interface string               `json:"public_ipv6_interface"`
	IPv6Prefixes        []lxc.IPv6PrefixInfo `json:"ipv6_prefixes"`
}

type DiskIOInfo struct {
	ReadBytes  uint64  `json:"read_bytes"`
	WriteBytes uint64  `json:"write_bytes"`
	ReadBps    float64 `json:"read_bps"`
	WriteBps   float64 `json:"write_bps"`
}

type HostMetricPoint struct {
	TS           int64   `json:"ts"`
	CPU          float64 `json:"cpu"`
	Memory       float64 `json:"memory"`
	Network      float64 `json:"network"`
	NetworkRx    float64 `json:"network_rx"`
	NetworkTx    float64 `json:"network_tx"`
	DiskIO       float64 `json:"disk_io"`
	DiskRead     float64 `json:"disk_read"`
	DiskWrite    float64 `json:"disk_write"`
	DiskUsagePct float64 `json:"disk_usage_pct"`
}

var hostCPUMu sync.Mutex
var lastHostCPU cpuTimes
var hostIOMu sync.Mutex
var lastHostIO hostIOSample
var hostMetricSamplerOnce sync.Once
var hostMetricMu sync.RWMutex
var hostMetricHistory []HostMetricPoint
var egressIPv4Mu sync.Mutex
var cachedEgressIPv4 lxc.PublicIPInfo
var cachedEgressIPv4At time.Time

const (
	hostMetricSampleInterval = 30 * time.Second
	hostMetricRetention      = 7 * 24 * time.Hour
)

type cpuTimes struct {
	Total uint64
	Idle  uint64
}

type hostIOSample struct {
	RXBytes    uint64
	TXBytes    uint64
	ReadBytes  uint64
	WriteBytes uint64
	At         int64
}

func getHostInfo() HostInfo {
	return getHostInfoWithNetworkDetails(true)
}

func getHostInfoWithNetworkDetails(includeDetails bool) HostInfo {
	info := HostInfo{
		CPU: CpuInfo{Cores: runtime.NumCPU()},
	}

	info.RAM = getMemoryInfo()
	info.Disk = getDiskInfo()
	info.CPU.Usage = getCPUUsage()
	info.Network, info.DiskIO = getHostRates(includeDetails)
	info.Load = getLoadInfo()
	info.Runtime = detectRuntimeProbeQuick()
	return info
}

func detectRuntimeProbeQuick() HostRuntimeProbe {
	devKVM := fileExists("/dev/kvm")
	nested, detail := detectNestedVirtualization()
	lxcOK := commandExists("lxc-create")
	kvmSupportedArch := runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64"
	kvmOK := kvmSupportedArch && devKVM && commandExists("virsh") && commandExists(kvmQEMUCheckKey())
	probe := HostRuntimeProbe{
		LXCAvailable:         lxcOK,
		KVMAvailable:         kvmOK,
		DevKVM:               devKVM,
		NestedVirtualization: nested,
		NestedDetail:         detail,
		SupportMode:          "unsupported",
	}
	if probe.KVMAvailable {
		probe.SupportMode = "kvm_lxc"
	} else if probe.LXCAvailable {
		probe.SupportMode = "lxc_only"
	}
	return probe
}

func StartHostMetricSampler() {
	hostMetricSamplerOnce.Do(func() {
		restoreHostMetricHistory()
		appendHostMetricPoint(getHostInfoWithNetworkDetails(false))
		go func() {
			ticker := time.NewTicker(hostMetricSampleInterval)
			defer ticker.Stop()
			for range ticker.C {
				appendHostMetricPoint(getHostInfoWithNetworkDetails(false))
			}
		}()
	})
}

func appendHostMetricPoint(info HostInfo) {
	memoryPct := 0.0
	if info.RAM.TotalMB > 0 {
		memoryPct = clampPercent(float64(info.RAM.UsedMB) / float64(info.RAM.TotalMB) * 100)
	}
	diskUsagePct := 0.0
	if info.Disk.TotalGB > 0 {
		diskUsagePct = clampPercent(info.Disk.UsedGB / info.Disk.TotalGB * 100)
	}
	point := HostMetricPoint{
		TS:           time.Now().UnixMilli(),
		CPU:          clampPercent(info.CPU.Usage),
		Memory:       memoryPct,
		NetworkRx:    info.Network.RXBps,
		NetworkTx:    info.Network.TXBps,
		Network:      info.Network.RXBps + info.Network.TXBps,
		DiskRead:     info.DiskIO.ReadBps,
		DiskWrite:    info.DiskIO.WriteBps,
		DiskIO:       info.DiskIO.ReadBps + info.DiskIO.WriteBps,
		DiskUsagePct: diskUsagePct,
	}
	cutoff := time.Now().Add(-hostMetricRetention).UnixMilli()
	persistMetricPoint("host", "host", point.TS, point)

	hostMetricMu.Lock()
	defer hostMetricMu.Unlock()

	keepFrom := 0
	for keepFrom < len(hostMetricHistory) && hostMetricHistory[keepFrom].TS < cutoff {
		keepFrom++
	}
	if keepFrom > 0 {
		copy(hostMetricHistory, hostMetricHistory[keepFrom:])
		hostMetricHistory = hostMetricHistory[:len(hostMetricHistory)-keepFrom]
	}
	if len(hostMetricHistory) > 0 && hostMetricHistory[len(hostMetricHistory)-1].TS/30_000 == point.TS/30_000 {
		hostMetricHistory[len(hostMetricHistory)-1] = point
	} else {
		hostMetricHistory = append(hostMetricHistory, point)
	}
}

func getHostMetricHistory() []HostMetricPoint {
	hostMetricMu.RLock()
	defer hostMetricMu.RUnlock()

	result := make([]HostMetricPoint, len(hostMetricHistory))
	copy(result, hostMetricHistory)
	return result
}

func clampPercent(value float64) float64 {
	if value < 0 || !isFiniteFloat(value) {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}

func isFiniteFloat(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func getMemoryInfo() MemoryInfo {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return MemoryInfo{TotalMB: 0, UsedMB: 0, FreeMB: 0}
	}
	defer f.Close()

	var total, available, free int64
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		val, _ := strconv.ParseInt(fields[1], 10, 64)
		switch fields[0] {
		case "MemTotal:":
			total = val / 1024
		case "MemAvailable:":
			available = val / 1024
		case "MemFree:":
			free = val / 1024
		}
	}

	used := total - available
	if available == 0 {
		used = total - free
	}

	return MemoryInfo{
		TotalMB: total,
		UsedMB:  used,
		FreeMB:  available,
	}
}

func getDiskInfo() DiskInfo {
	if info, ok := getRootDiskInfo(); ok {
		return info
	}

	{
		// Try command-based fallback
		cmd := exec.Command("df", "-BG", "/")
		output, err := cmd.Output()
		if err == nil {
			lines := strings.Split(string(output), "\n")
			if len(lines) >= 2 {
				fields := strings.Fields(lines[1])
				if len(fields) >= 4 {
					total, _ := parseSizeGBf(fields[1])
					used, _ := parseSizeGBf(fields[2])
					free, _ := parseSizeGBf(fields[3])
					return DiskInfo{TotalGB: total, UsedGB: used, FreeGB: free}
				}
			}
		}
		return DiskInfo{}
	}
}

func getCPUUsage() float64 {
	current, err := readCPUTimes()
	if err != nil {
		return 0
	}

	hostCPUMu.Lock()
	defer hostCPUMu.Unlock()

	if lastHostCPU.Total == 0 {
		lastHostCPU = current
		return 0
	}

	totalDelta := current.Total - lastHostCPU.Total
	idleDelta := current.Idle - lastHostCPU.Idle
	lastHostCPU = current

	if totalDelta == 0 {
		return 0
	}

	usage := (1 - float64(idleDelta)/float64(totalDelta)) * 100
	if usage < 0 {
		return 0
	}
	if usage > 100 {
		return 100
	}
	return usage
}

func readCPUTimes() (cpuTimes, error) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return cpuTimes{}, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	if !scanner.Scan() {
		return cpuTimes{}, scanner.Err()
	}

	fields := strings.Fields(scanner.Text())
	if len(fields) < 8 || fields[0] != "cpu" {
		return cpuTimes{}, nil
	}

	var values []uint64
	for _, field := range fields[1:] {
		value, _ := strconv.ParseUint(field, 10, 64)
		values = append(values, value)
	}

	var total uint64
	for _, value := range values {
		total += value
	}

	idle := values[3]
	if len(values) > 4 {
		idle += values[4]
	}

	return cpuTimes{Total: total, Idle: idle}, nil
}

func parseSizeGB(s string) (int64, error) {
	s = strings.TrimSuffix(s, "G")
	s = strings.TrimSpace(s)
	val, err := strconv.ParseInt(s, 10, 64)
	return val, err
}

func parseSizeGBf(s string) (float64, error) {
	s = strings.TrimSuffix(s, "G")
	s = strings.TrimSpace(s)
	val, err := strconv.ParseFloat(s, 64)
	return val, err
}

func getHostRates(includeDetails bool) (NetworkInfo, DiskIOInfo) {
	rx, tx := readHostNetworkBytes()
	readBytes, writeBytes := readHostDiskBytes()
	now := unixNano()

	network := NetworkInfo{RXBytes: rx, TXBytes: tx}
	if includeDetails {
		publicIPv4 := detectDisplayPublicIPv4()
		network.PublicIPv4 = publicIPv4.Address
		network.PublicIPv4Interface = publicIPv4.Interface
		network.PublicIPv4Addresses = lxc.DetectFreePublicIPv4Candidates(0)
		network.IPv6Prefixes = lxc.DetectHostPublicIPv6Prefixes()
		if len(network.IPv6Prefixes) > 0 {
			network.PublicIPv6 = network.IPv6Prefixes[0].Address
			network.PublicIPv6Interface = network.IPv6Prefixes[0].Interface
		}
	}
	diskIO := DiskIOInfo{ReadBytes: readBytes, WriteBytes: writeBytes}

	hostIOMu.Lock()
	defer hostIOMu.Unlock()

	if lastHostIO.At == 0 {
		lastHostIO = hostIOSample{RXBytes: rx, TXBytes: tx, ReadBytes: readBytes, WriteBytes: writeBytes, At: now}
		return network, diskIO
	}

	elapsed := float64(now-lastHostIO.At) / 1_000_000_000
	if elapsed > 0 {
		if rx >= lastHostIO.RXBytes {
			network.RXBps = float64(rx-lastHostIO.RXBytes) / elapsed
		}
		if tx >= lastHostIO.TXBytes {
			network.TXBps = float64(tx-lastHostIO.TXBytes) / elapsed
		}
		if readBytes >= lastHostIO.ReadBytes {
			diskIO.ReadBps = float64(readBytes-lastHostIO.ReadBytes) / elapsed
		}
		if writeBytes >= lastHostIO.WriteBytes {
			diskIO.WriteBps = float64(writeBytes-lastHostIO.WriteBytes) / elapsed
		}
	}

	lastHostIO = hostIOSample{RXBytes: rx, TXBytes: tx, ReadBytes: readBytes, WriteBytes: writeBytes, At: now}
	return network, diskIO
}

func readHostNetworkBytes() (uint64, uint64) {
	ifaces := detectHostTrafficInterfaces()
	if len(ifaces) == 0 {
		ifaces = fallbackHostTrafficInterfaces()
	}

	var rx, tx uint64
	for _, name := range ifaces {
		rx += readUintFile("/sys/class/net/" + name + "/statistics/rx_bytes")
		tx += readUintFile("/sys/class/net/" + name + "/statistics/tx_bytes")
	}
	return rx, tx
}

func detectHostTrafficInterfaces() []string {
	seen := map[string]bool{}
	result := make([]string, 0, 2)
	add := func(name string) {
		name = strings.TrimSpace(name)
		if !isHostTrafficInterface(name) || seen[name] {
			return
		}
		seen[name] = true
		result = append(result, name)
	}

	if iface, _ := detectDefaultIPv4Route(); iface != "" {
		add(iface)
	}
	if iface, _ := detectDefaultIPv6Route(); iface != "" {
		add(iface)
	}
	if pub := lxc.DetectPublicIPv4(); pub.Interface != "" {
		add(pub.Interface)
	}
	for _, prefix := range lxc.DetectHostPublicIPv6Prefixes() {
		add(prefix.Interface)
	}
	return result
}

func fallbackHostTrafficInterfaces() []string {
	entries, err := os.ReadDir("/sys/class/net")
	if err != nil {
		return nil
	}

	result := make([]string, 0)
	for _, entry := range entries {
		name := entry.Name()
		if !isHostTrafficInterface(name) {
			continue
		}
		state := strings.TrimSpace(readFirstExistingFile(filepath.Join("/sys/class/net", name, "operstate")))
		if state == "down" {
			continue
		}
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

func isHostTrafficInterface(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" || name == "lo" {
		return false
	}
	if isContainerLikeInterfaceName(name) {
		return false
	}
	return true
}

func readHostDiskBytes() (uint64, uint64) {
	f, err := os.Open("/proc/diskstats")
	if err != nil {
		return 0, 0
	}
	defer f.Close()

	var readSectors, writeSectors uint64
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 14 {
			continue
		}
		device := fields[2]
		if strings.HasPrefix(device, "loop") ||
			strings.HasPrefix(device, "ram") ||
			strings.HasPrefix(device, "fd") ||
			strings.HasPrefix(device, "sr") {
			continue
		}
		read, _ := strconv.ParseUint(fields[5], 10, 64)
		write, _ := strconv.ParseUint(fields[9], 10, 64)
		readSectors += read
		writeSectors += write
	}
	return readSectors * 512, writeSectors * 512
}

func readUintFile(path string) uint64 {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	value, _ := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
	return value
}

func unixNano() int64 {
	return time.Now().UnixNano()
}

func getLoadInfo() LoadInfo {
	f, err := os.Open("/proc/loadavg")
	if err != nil {
		return LoadInfo{}
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	if !scanner.Scan() {
		return LoadInfo{}
	}

	fields := strings.Fields(scanner.Text())
	if len(fields) < 3 {
		return LoadInfo{}
	}

	load1, _ := strconv.ParseFloat(fields[0], 64)
	load5, _ := strconv.ParseFloat(fields[1], 64)
	load15, _ := strconv.ParseFloat(fields[2], 64)
	return LoadInfo{Load1: load1, Load5: load5, Load15: load15}
}

func HandleHostReport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
		return
	}
	if !requireScope(w, r, "host:read") {
		return
	}
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: getHostProbeReport()})
}

func getHostProbeReport() HostProbeReport {
	host, _ := os.Hostname()
	mem := getMemoryInfo()
	report := HostProbeReport{
		GeneratedAt:       time.Now().Format("2006-01-02 15:04:05"),
		Hostname:          host,
		Kernel:            strings.TrimSpace(runCommandOutput(2*time.Second, "uname", "-srmo")),
		OS:                detectOSRelease(),
		CPU:               detectHostCPUProbe(),
		Memory:            HostMemoryProbe{TotalMB: mem.TotalMB, UsedMB: mem.UsedMB, FreeMB: mem.FreeMB, Modules: detectMemoryModules()},
		Disks:             detectHostDisks(),
		NetworkInterfaces: detectHostNICs(),
		PublicIPv4:        detectAllPublicIPv4(),
		IPv6Prefixes:      lxc.DetectHostPublicIPv6Prefixes(),
		Gateways:          detectGateways(),
		GPUs:              detectGPUs(),
		System:            detectSystemProbe(),
		Environment:       detectHostEnvironment(),
	}
	report.IPv6Addresses = collectIPv6Addresses(report.NetworkInterfaces)
	report.IPv4Addresses = collectIPv4Addresses(report.NetworkInterfaces)
	report.IPv4Prefixes = detectPublicIPv4Prefixes(report.NetworkInterfaces, report.Gateways)
	report.Runtime = detectRuntimeProbe(report.Environment)
	report.CPU.HasIntegratedGPU = hasIntegratedGPU(report.GPUs)
	return report
}

func detectOSRelease() string {
	values := readKeyValueFile("/etc/os-release", "=")
	if pretty := trimOSReleaseValue(values["PRETTY_NAME"]); pretty != "" {
		return pretty
	}
	if name := trimOSReleaseValue(values["NAME"]); name != "" {
		return name
	}
	return strings.TrimSpace(runCommandOutput(2*time.Second, "uname", "-o"))
}

func trimOSReleaseValue(value string) string {
	return strings.Trim(strings.TrimSpace(value), `"`)
}

func detectHostCPUProbe() HostCPUProbe {
	probe := HostCPUProbe{Cores: runtime.NumCPU(), Threads: runtime.NumCPU(), Architecture: runtime.GOARCH}
	armImplementer := ""
	armPart := ""
	if data, err := os.ReadFile("/proc/cpuinfo"); err == nil {
		seenFlags := map[string]bool{}
		for _, line := range strings.Split(string(data), "\n") {
			fields := strings.SplitN(line, ":", 2)
			if len(fields) != 2 {
				continue
			}
			key := strings.TrimSpace(fields[0])
			keyLower := strings.ToLower(key)
			value := strings.TrimSpace(fields[1])
			switch keyLower {
			case "model name", "hardware", "processor":
				if probe.Model == "" && meaningfulCPUModel(value) {
					probe.Model = value
				}
			case "cpu cores":
				if cores, err := strconv.Atoi(value); err == nil && cores > probe.Cores {
					probe.Cores = cores
				}
			case "cpu implementer":
				if armImplementer == "" {
					armImplementer = strings.ToLower(value)
				}
			case "cpu part":
				if armPart == "" {
					armPart = strings.ToLower(value)
				}
			case "flags", "features":
				for _, flag := range strings.Fields(value) {
					if flag == "vmx" || flag == "svm" || flag == "virt" {
						probe.Virtualization = true
						probe.VirtualizationKey = flag
					}
					if !seenFlags[flag] {
						seenFlags[flag] = true
						probe.Flags = append(probe.Flags, flag)
					}
				}
			}
		}
		sort.Strings(probe.Flags)
	}
	enrichCPUProbeFromLscpu(&probe, &armImplementer, &armPart)
	if probe.Model == "" {
		probe.Model = armCPUModelName(armImplementer, armPart)
	}
	if probe.Model == "" && runtime.GOARCH == "arm64" {
		probe.Model = "ARM64 CPU"
	}
	if probe.Model == "" {
		probe.Model = "Unknown"
	}
	return probe
}

func meaningfulCPUModel(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	if _, err := strconv.Atoi(value); err == nil {
		return false
	}
	lower := strings.ToLower(value)
	return lower != "unknown" && lower != "not specified"
}

func enrichCPUProbeFromLscpu(probe *HostCPUProbe, armImplementer *string, armPart *string) {
	out := runCommandOutput(2*time.Second, "lscpu")
	if out == "" {
		return
	}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.SplitN(line, ":", 2)
		if len(fields) != 2 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(fields[0]))
		value := strings.TrimSpace(fields[1])
		switch key {
		case "model name":
			if probe.Model == "" && meaningfulCPUModel(value) {
				probe.Model = value
			}
		case "cpu(s)":
			if threads, err := strconv.Atoi(value); err == nil && threads > probe.Threads {
				probe.Threads = threads
			}
		case "core(s) per socket":
			if cores, err := strconv.Atoi(value); err == nil && cores > 0 {
				probe.Cores = cores
			}
		case "socket(s)":
			if sockets, err := strconv.Atoi(value); err == nil && sockets > 1 && probe.Cores > 0 {
				probe.Cores *= sockets
			}
		case "virtualization":
			lower := strings.ToLower(value)
			if value != "" && lower != "none" && lower != "n/a" {
				probe.Virtualization = true
				probe.VirtualizationKey = value
			}
		case "flags":
			seen := map[string]bool{}
			for _, flag := range probe.Flags {
				seen[flag] = true
			}
			for _, flag := range strings.Fields(value) {
				if flag == "vmx" || flag == "svm" || flag == "virt" {
					probe.Virtualization = true
					probe.VirtualizationKey = flag
				}
				if !seen[flag] {
					probe.Flags = append(probe.Flags, flag)
					seen[flag] = true
				}
			}
			sort.Strings(probe.Flags)
		case "cpu implementer":
			if *armImplementer == "" {
				*armImplementer = strings.ToLower(value)
			}
		case "cpu part":
			if *armPart == "" {
				*armPart = strings.ToLower(value)
			}
		}
	}
}

func armCPUModelName(implementer, part string) string {
	implementer = normalizeHexID(implementer)
	part = normalizeHexID(part)
	if implementer == "" || part == "" {
		return ""
	}
	armParts := map[string]string{
		"0x41:0xd03": "ARM Cortex-A53",
		"0x41:0xd05": "ARM Cortex-A55",
		"0x41:0xd07": "ARM Cortex-A57",
		"0x41:0xd08": "ARM Cortex-A72",
		"0x41:0xd09": "ARM Cortex-A73",
		"0x41:0xd0a": "ARM Cortex-A75",
		"0x41:0xd0b": "ARM Cortex-A76",
		"0x41:0xd0c": "ARM Neoverse N1",
		"0x41:0xd0d": "ARM Cortex-A77",
		"0x41:0xd40": "ARM Neoverse V1",
		"0x41:0xd41": "ARM Cortex-A78",
		"0x41:0xd49": "ARM Neoverse N2",
		"0x41:0xd4f": "ARM Neoverse V2",
	}
	if model := armParts[implementer+":"+part]; model != "" {
		return model
	}
	return strings.ToUpper(strings.TrimPrefix(implementer, "0x")) + " ARM CPU part " + part
}

func normalizeHexID(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	if strings.HasPrefix(value, "0x") {
		return value
	}
	return "0x" + value
}

func detectMemoryModules() []HostMemoryModule {
	if !commandExists("dmidecode") {
		return nil
	}
	out := runCommandOutput(4*time.Second, "dmidecode", "-t", "memory")
	modules := make([]HostMemoryModule, 0)
	var current HostMemoryModule
	inDevice := false
	flush := func() {
		if !inDevice {
			return
		}
		if current.Size != "" && !strings.EqualFold(current.Size, "No Module Installed") {
			modules = append(modules, current)
		}
		current = HostMemoryModule{}
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "Memory Device") {
			flush()
			inDevice = true
			continue
		}
		if !inDevice {
			continue
		}
		parts := strings.SplitN(strings.TrimSpace(line), ":", 2)
		if len(parts) != 2 {
			continue
		}
		key, value := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
		switch key {
		case "Locator":
			current.Locator = value
		case "Size":
			current.Size = value
		case "Type":
			current.Type = value
		case "Speed":
			current.Speed = value
		case "Manufacturer":
			current.Manufacturer = value
		case "Part Number":
			current.PartNumber = value
		case "Serial Number":
			current.SerialNumber = value
		}
	}
	flush()
	return modules
}

func detectHostDisks() []HostDiskProbe {
	disks := make([]HostDiskProbe, 0)
	entries, err := os.ReadDir("/sys/block")
	if err != nil {
		return disks
	}
	mounts := detectMountpointsByDevice()
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, "loop") || strings.HasPrefix(name, "ram") || strings.HasPrefix(name, "fd") || strings.HasPrefix(name, "sr") {
			continue
		}
		base := filepath.Join("/sys/block", name)
		path := "/dev/" + name
		model := strings.TrimSpace(readFirstExistingFile(filepath.Join(base, "device/model"), filepath.Join(base, "device/name")))
		vendor := strings.TrimSpace(readFirstExistingFile(filepath.Join(base, "device/vendor")))
		virtual := isVirtualBlockDevice(name, model, vendor)
		disk := HostDiskProbe{
			Name:        name,
			Path:        path,
			Model:       model,
			Serial:      strings.TrimSpace(readFirstExistingFile(filepath.Join(base, "device/serial"), filepath.Join(base, "serial"))),
			SizeBytes:   readUintFile(filepath.Join(base, "size")) * 512,
			Type:        detectDiskType(base, name, virtual),
			Virtual:     virtual,
			Rotational:  strings.TrimSpace(readFirstExistingFile(filepath.Join(base, "queue/rotational"))) == "1",
			Mountpoints: mounts[name],
		}
		if !virtual {
			disk.SMART = detectDiskSMART(path)
		}
		disk.Health = disk.SMARTHealth()
		disk.HealthDetail = disk.SMARTDetail()
		disks = append(disks, disk)
	}
	sort.Slice(disks, func(i, j int) bool { return disks[i].Name < disks[j].Name })
	return disks
}

func detectDiskType(base, name string, virtual bool) string {
	if virtual {
		return "Virtual"
	}
	if strings.HasPrefix(name, "nvme") {
		return "NVMe"
	}
	if strings.TrimSpace(readFirstExistingFile(filepath.Join(base, "queue/rotational"))) == "1" {
		return "HDD"
	}
	return "SSD"
}

func isVirtualBlockDevice(name, model, vendor string) bool {
	lower := strings.ToLower(strings.TrimSpace(name + " " + model + " " + vendor))
	if strings.HasPrefix(name, "vd") || strings.HasPrefix(name, "xvd") {
		return true
	}
	for _, token := range []string{
		"qemu", "virtio", "virtual", "vmware", "vbox", "xen",
		"amazon elastic block store", "google persistentdisk", "microsoft", "blockvolume",
	} {
		if strings.Contains(lower, token) {
			return true
		}
	}
	return false
}

func (disk HostDiskProbe) SMARTHealth() string {
	if disk.Virtual {
		return "virtual"
	}
	if disk.SMART.Available && disk.Health != "" {
		return disk.Health
	}
	return disk.SMART.Health()
}

func (disk HostDiskProbe) SMARTDetail() string {
	if disk.Virtual {
		return "虚拟磁盘，真实 SMART/寿命/通电数据需在物理宿主机查看"
	}
	return disk.SMART.Detail()
}

func (smart HostDiskSMARTProbe) Health() string {
	if !smart.Available {
		return "unknown"
	}
	if smart.MediaErrors > 0 {
		return "failed"
	}
	return "ok"
}

func (smart HostDiskSMARTProbe) Detail() string {
	if !smart.Available {
		return "smartctl not installed or no SMART output"
	}
	parts := []string{"SMART passed"}
	if smart.LifeUsedPercent != nil {
		parts = append(parts, fmt.Sprintf("寿命已用 %d%%", *smart.LifeUsedPercent))
	}
	if smart.PowerOnHours > 0 {
		parts = append(parts, fmt.Sprintf("通电 %dh", smart.PowerOnHours))
	}
	if smart.WrittenDataBytes > 0 {
		parts = append(parts, fmt.Sprintf("写入 %s", formatBytesText(smart.WrittenDataBytes)))
	}
	if smart.ReadDataBytes > 0 {
		parts = append(parts, fmt.Sprintf("读取 %s", formatBytesText(smart.ReadDataBytes)))
	}
	if smart.MediaErrors > 0 {
		parts = append(parts, fmt.Sprintf("介质错误 %d", smart.MediaErrors))
	}
	return strings.Join(parts, " | ")
}

func detectDiskSMART(path string) HostDiskSMARTProbe {
	smart := HostDiskSMARTProbe{}
	if !commandExists("smartctl") {
		return smart
	}
	out := runCommandOutput(8*time.Second, "smartctl", "-a", "-j", path)
	if strings.TrimSpace(out) == "" {
		return smart
	}
	var data smartctlOutput
	if err := json.Unmarshal([]byte(out), &data); err != nil {
		return smart
	}
	smart.Available = true
	smart.PowerOnHours = data.PowerOnTime.Hours
	smart.PowerCycleCount = data.PowerCycleCount
	if data.NVMe.PowerOnHours > 0 {
		smart.PowerOnHours = uint64ToInt64(data.NVMe.PowerOnHours)
	}
	if data.NVMe.PowerCycles > 0 {
		smart.PowerCycleCount = uint64ToInt64(data.NVMe.PowerCycles)
	}
	if data.NVMe.PercentageUsed > 0 {
		if used, ok := smartPercentToInt(data.NVMe.PercentageUsed); ok {
			smart.LifeUsedPercent = &used
		}
	}
	smart.ReadDataBytes = data.NVMe.DataUnitsRead * 512000
	smart.WrittenDataBytes = data.NVMe.DataUnitsWritten * 512000
	smart.ReadCommands = data.NVMe.HostReadCommands
	smart.WriteCommands = data.NVMe.HostWriteCommands
	smart.MediaErrors = data.NVMe.MediaErrors

	parseATAAttributes(&smart, data.ATASmartAttributes.Table)
	if data.SmartStatus != nil && !data.SmartStatus.Passed {
		smart.MediaErrors++
	}
	return smart
}

type smartctlOutput struct {
	SmartStatus *struct {
		Passed bool `json:"passed"`
	} `json:"smart_status"`
	PowerOnTime struct {
		Hours int64 `json:"hours"`
	} `json:"power_on_time"`
	PowerCycleCount    int64 `json:"power_cycle_count"`
	ATASmartAttributes struct {
		Table []smartctlAttribute `json:"table"`
	} `json:"ata_smart_attributes"`
	NVMe struct {
		PercentageUsed    uint64 `json:"percentage_used"`
		DataUnitsRead     uint64 `json:"data_units_read"`
		DataUnitsWritten  uint64 `json:"data_units_written"`
		HostReadCommands  uint64 `json:"host_reads"`
		HostWriteCommands uint64 `json:"host_writes"`
		PowerOnHours      uint64 `json:"power_on_hours"`
		PowerCycles       uint64 `json:"power_cycles"`
		MediaErrors       uint64 `json:"media_errors"`
	} `json:"nvme_smart_health_information_log"`
}

type smartctlAttribute struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Value int    `json:"value"`
	Raw   struct {
		Value  json.Number `json:"value"`
		String string      `json:"string"`
	} `json:"raw"`
}

func parseATAAttributes(smart *HostDiskSMARTProbe, attrs []smartctlAttribute) {
	for _, attr := range attrs {
		name := normalizeSMARTAttrName(attr.Name)
		raw := smartAttrRawUint(attr)
		rawText := smartAttrRawText(attr)
		switch name {
		case "poweronhours":
			if smart.PowerOnHours == 0 {
				if parsed, ok := smartAttrRawInt64(attr); ok {
					smart.PowerOnHours = parsed
				}
			}
		case "powercyclecount":
			if smart.PowerCycleCount == 0 {
				if parsed, ok := smartAttrRawInt64(attr); ok {
					smart.PowerCycleCount = parsed
				}
			}
		case "totallbaswritten":
			if smart.WrittenDataBytes == 0 {
				smart.WrittenDataBytes = raw * 512
			}
		case "totallbasread":
			if smart.ReadDataBytes == 0 {
				smart.ReadDataBytes = raw * 512
			}
		case "hostwrites32mib":
			if smart.WrittenDataBytes == 0 {
				smart.WrittenDataBytes = raw * 32 * 1024 * 1024
			}
		case "hostreads32mib":
			if smart.ReadDataBytes == 0 {
				smart.ReadDataBytes = raw * 32 * 1024 * 1024
			}
		case "hostwritecommands":
			smart.WriteCommands = raw
		case "hostreadcommands":
			smart.ReadCommands = raw
		case "wearlevelingcount":
			smart.WearLevelingCount = rawText
			if smart.LifeUsedPercent == nil && attr.Value > 0 && attr.Value <= 100 {
				used := 100 - attr.Value
				if used < 0 {
					used = 0
				}
				smart.LifeUsedPercent = &used
			}
		case "percentlifetimeremain", "mediawearoutindicator":
			if smart.LifeUsedPercent == nil {
				remaining := attr.Value
				if raw > 0 && raw <= 100 {
					if parsed, ok := smartPercentToInt(raw); ok {
						remaining = parsed
					}
				}
				used := 100 - remaining
				if used < 0 {
					used = 0
				}
				if used <= 100 {
					smart.LifeUsedPercent = &used
				}
			}
		case "percentageused":
			if smart.LifeUsedPercent == nil && raw <= 255 {
				if used, ok := smartPercentToInt(raw); ok {
					smart.LifeUsedPercent = &used
				}
			}
		case "erasefailcounttotal", "erasecount", "nandwrites", "programfailcnttotal":
			if smart.EraseCount == "" && rawText != "" {
				smart.EraseCount = rawText
			}
		case "mediaerrors":
			smart.MediaErrors = raw
		}
	}
}

func uint64ToInt64(value uint64) int64 {
	if value > 9223372036854775807 {
		return 0
	}
	return int64(value)
}

func smartPercentToInt(value uint64) (int, bool) {
	if value > 2147483647 {
		return 0, false
	}
	return int(value), true
}

func normalizeSMARTAttrName(name string) string {
	name = strings.ToLower(name)
	var b strings.Builder
	for _, ch := range name {
		if ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' {
			b.WriteRune(ch)
		}
	}
	return b.String()
}

func smartAttrRawText(attr smartctlAttribute) string {
	if attr.Raw.String != "" {
		return attr.Raw.String
	}
	if attr.Raw.Value != "" {
		return attr.Raw.Value.String()
	}
	return ""
}

func smartAttrRawInt64(attr smartctlAttribute) (int64, bool) {
	text := smartAttrRawText(attr)
	if value, err := strconv.ParseInt(text, 10, 64); err == nil {
		return value, true
	}
	digits := firstUintText(text)
	if digits == "" {
		return 0, false
	}
	value, err := strconv.ParseInt(digits, 10, 64)
	if err != nil {
		return 0, false
	}
	return value, true
}

func smartAttrRawUint(attr smartctlAttribute) uint64 {
	text := smartAttrRawText(attr)
	if value, err := strconv.ParseUint(text, 10, 64); err == nil {
		return value
	}
	digits := firstUintText(text)
	if digits == "" {
		return 0
	}
	value, _ := strconv.ParseUint(digits, 10, 64)
	return value
}

func firstUintText(value string) string {
	start := -1
	for i, ch := range value {
		if ch >= '0' && ch <= '9' {
			if start < 0 {
				start = i
			}
			continue
		}
		if start >= 0 {
			return value[start:i]
		}
	}
	if start >= 0 {
		return value[start:]
	}
	return ""
}

func detectMountpointsByDevice() map[string][]string {
	result := map[string][]string{}
	f, err := os.Open("/proc/mounts")
	if err != nil {
		return result
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 || !strings.HasPrefix(fields[0], "/dev/") {
			continue
		}
		dev := strings.TrimPrefix(filepath.Base(fields[0]), "/dev/")
		parent := diskParentName(dev)
		result[parent] = append(result[parent], fields[1])
	}
	return result
}

func diskParentName(dev string) string {
	for _, suffix := range []string{"p1", "p2", "p3", "p4", "p5", "p6", "p7", "p8", "p9"} {
		if strings.HasSuffix(dev, suffix) && strings.HasPrefix(dev, "nvme") {
			return strings.TrimSuffix(dev, suffix)
		}
	}
	for len(dev) > 0 && dev[len(dev)-1] >= '0' && dev[len(dev)-1] <= '9' {
		dev = dev[:len(dev)-1]
	}
	return dev
}

func detectHostNICs() []HostNICProbe {
	entries, err := os.ReadDir("/sys/class/net")
	if err != nil {
		return nil
	}
	ipv4, ipv6 := detectInterfaceIPs()
	nics := make([]HostNICProbe, 0)
	for _, entry := range entries {
		name := entry.Name()
		if name == "lo" {
			continue
		}
		base := filepath.Join("/sys/class/net", name)
		speed, _ := strconv.Atoi(strings.TrimSpace(readFirstExistingFile(filepath.Join(base, "speed"))))
		nic := HostNICProbe{
			Name:      name,
			MAC:       strings.TrimSpace(readFirstExistingFile(filepath.Join(base, "address"))),
			State:     strings.TrimSpace(readFirstExistingFile(filepath.Join(base, "operstate"))),
			SpeedMbps: speed,
			Driver:    strings.TrimSpace(runCommandOutput(2*time.Second, "sh", "-c", fmt.Sprintf("basename $(readlink -f /sys/class/net/%s/device/driver 2>/dev/null) 2>/dev/null", shellQuoteSimple(name)))),
			Model:     detectNICModel(name),
			IPv4:      ipv4[name],
			IPv6:      ipv6[name],
		}
		nics = append(nics, nic)
	}
	sort.Slice(nics, func(i, j int) bool { return nics[i].Name < nics[j].Name })
	return nics
}

func detectNICModel(name string) string {
	out := runCommandOutput(2*time.Second, "sh", "-c", fmt.Sprintf("lspci -D 2>/dev/null | grep -iE 'ethernet|network' | head -n 1 || true"))
	if out != "" {
		return strings.TrimSpace(out)
	}
	return strings.TrimSpace(readFirstExistingFile(filepath.Join("/sys/class/net", name, "device", "uevent")))
}

func detectInterfaceIPs() (map[string][]HostIPProbe, map[string][]HostIPProbe) {
	ipv4 := map[string][]HostIPProbe{}
	ipv6 := map[string][]HostIPProbe{}
	for _, family := range []struct {
		arg string
		dst map[string][]HostIPProbe
	}{{"-4", ipv4}, {"-6", ipv6}} {
		out := runCommandOutput(3*time.Second, "ip", "-o", family.arg, "addr", "show")
		for _, line := range strings.Split(out, "\n") {
			fields := strings.Fields(line)
			if len(fields) < 4 {
				continue
			}
			iface := strings.TrimSuffix(fields[1], ":")
			addr := fields[3]
			ip, network, err := net.ParseCIDR(addr)
			if err != nil || ip == nil || network == nil {
				continue
			}
			ones, _ := network.Mask.Size()
			scope := ""
			for i, field := range fields {
				if field == "scope" && i+1 < len(fields) {
					scope = fields[i+1]
				}
			}
			family.dst[iface] = append(family.dst[iface], HostIPProbe{
				Interface: iface,
				Address:   ip.String(),
				PrefixLen: ones,
				Scope:     scope,
			})
		}
	}
	return ipv4, ipv6
}

func detectAllPublicIPv4() []string {
	seen := map[string]bool{}
	result := make([]string, 0)
	if pub := lxc.DetectPublicIPv4(); pub.Address != "" {
		if ip := net.ParseIP(pub.Address); isPublicIPv4(ip) {
			seen[pub.Address] = true
			result = append(result, pub.Address)
		}
	}
	out := runCommandOutput(3*time.Second, "ip", "-o", "-4", "addr", "show", "scope", "global")
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		ip, _, err := net.ParseCIDR(fields[3])
		if err != nil || ip == nil {
			continue
		}
		if !isPublicIPv4(ip) {
			continue
		}
		value := ip.String()
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	if egress := detectEgressPublicIPv4(); egress.Address != "" {
		if !seen[egress.Address] {
			seen[egress.Address] = true
			result = append(result, egress.Address)
		}
	}
	return result
}

func detectDisplayPublicIPv4() lxc.PublicIPInfo {
	if pub := lxc.DetectPublicIPv4(); pub.Address != "" {
		return pub
	}
	return detectEgressPublicIPv4()
}

func detectEgressPublicIPv4() lxc.PublicIPInfo {
	egressIPv4Mu.Lock()
	defer egressIPv4Mu.Unlock()

	if cachedEgressIPv4.Address != "" && time.Since(cachedEgressIPv4At) < 5*time.Minute {
		return cachedEgressIPv4
	}

	client := &http.Client{Timeout: 1200 * time.Millisecond}
	for _, endpoint := range []string{
		"https://api.ipify.org",
		"https://ifconfig.me/ip",
		"https://icanhazip.com",
	} {
		ctx, cancel := context.WithTimeout(context.Background(), 1200*time.Millisecond)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			cancel()
			continue
		}
		resp, err := client.Do(req)
		if err != nil {
			cancel()
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 128))
		_ = resp.Body.Close()
		cancel()
		if readErr != nil || resp.StatusCode < 200 || resp.StatusCode >= 300 {
			continue
		}
		address := strings.TrimSpace(string(body))
		ip := net.ParseIP(address)
		if !isPublicIPv4(ip) {
			continue
		}
		iface, gateway := detectDefaultIPv4Route()
		cachedEgressIPv4 = lxc.PublicIPInfo{
			Address:    ip.String(),
			Interface:  iface,
			Prefix:     ip.String() + "/32",
			PrefixLen:  32,
			SubnetMask: "255.255.255.255",
			Gateway:    gateway,
			IsTunnel:   isTunnelLikeInterfaceName(iface),
			Source:     "egress",
		}
		cachedEgressIPv4At = time.Now()
		return cachedEgressIPv4
	}

	cachedEgressIPv4 = lxc.PublicIPInfo{}
	cachedEgressIPv4At = time.Now()
	return cachedEgressIPv4
}

func detectDefaultIPv4Route() (string, string) {
	out := runCommandOutput(2*time.Second, "ip", "-4", "route", "show", "default")
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		iface := ""
		gateway := ""
		for i, field := range fields {
			if field == "dev" && i+1 < len(fields) {
				iface = fields[i+1]
			}
			if field == "via" && i+1 < len(fields) {
				gateway = fields[i+1]
			}
		}
		if iface != "" || gateway != "" {
			return iface, gateway
		}
	}
	return "", ""
}

func detectDefaultIPv6Route() (string, string) {
	out := runCommandOutput(2*time.Second, "ip", "-6", "route", "show", "default")
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		iface := ""
		gateway := ""
		for i, field := range fields {
			if field == "dev" && i+1 < len(fields) {
				iface = fields[i+1]
			}
			if field == "via" && i+1 < len(fields) {
				gateway = fields[i+1]
			}
		}
		if iface != "" || gateway != "" {
			return iface, gateway
		}
	}
	return "", ""
}

func collectIPv4Addresses(nics []HostNICProbe) []HostIPProbe {
	result := make([]HostIPProbe, 0)
	for _, nic := range nics {
		for _, ip := range nic.IPv4 {
			parsed := net.ParseIP(ip.Address)
			if isPublicIPv4(parsed) {
				result = append(result, ip)
			}
		}
	}
	return result
}

func detectPublicIPv4Prefixes(nics []HostNICProbe, gateways []HostGatewayProbe) []HostIPv4PrefixProbe {
	result := make([]HostIPv4PrefixProbe, 0)
	seen := map[string]bool{}
	gatewayByIface := map[string]string{}
	for _, gateway := range gateways {
		if gateway.Family == "ipv4" && gateway.Interface != "" && gateway.Gateway != "" {
			gatewayByIface[gateway.Interface] = gateway.Gateway
		}
	}
	add := func(item HostIPv4PrefixProbe) {
		if item.Prefix == "" || item.Interface == "" {
			return
		}
		key := item.Interface + "|" + item.Prefix
		if seen[key] {
			return
		}
		seen[key] = true
		result = append(result, item)
	}
	for _, nic := range nics {
		for _, ip := range nic.IPv4 {
			parsed := net.ParseIP(ip.Address)
			if !isPublicIPv4(parsed) || ip.PrefixLen <= 0 || ip.PrefixLen > 32 {
				continue
			}
			prefix, subnet := ipv4PrefixAndMask(parsed, ip.PrefixLen)
			add(HostIPv4PrefixProbe{
				Interface:  nic.Name,
				Address:    ip.Address,
				Prefix:     prefix,
				PrefixLen:  ip.PrefixLen,
				SubnetMask: subnet,
				Gateway:    gatewayByIface[nic.Name],
				Source:     "address",
			})
		}
	}
	for _, item := range detectIPv4RoutePrefixes(gatewayByIface) {
		add(item)
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Interface == result[j].Interface {
			return result[i].Prefix < result[j].Prefix
		}
		return result[i].Interface < result[j].Interface
	})
	return result
}

func detectIPv4RoutePrefixes(gatewayByIface map[string]string) []HostIPv4PrefixProbe {
	out := runCommandOutput(3*time.Second, "ip", "-4", "route", "show")
	result := make([]HostIPv4PrefixProbe, 0)
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] == "default" {
			continue
		}
		_, network, err := net.ParseCIDR(fields[0])
		if err != nil || network == nil || network.IP.To4() == nil {
			continue
		}
		if !isPublicIPv4(network.IP) {
			continue
		}
		iface := ""
		gateway := ""
		src := ""
		for i, field := range fields {
			if field == "dev" && i+1 < len(fields) {
				iface = fields[i+1]
			}
			if field == "via" && i+1 < len(fields) {
				gateway = fields[i+1]
			}
			if field == "src" && i+1 < len(fields) {
				src = fields[i+1]
			}
		}
		if iface == "" || isContainerLikeInterfaceName(iface) {
			continue
		}
		ones, bits := network.Mask.Size()
		if bits != 32 || ones <= 0 || ones > 32 {
			continue
		}
		if gateway == "" {
			gateway = gatewayByIface[iface]
		}
		prefix, subnet := ipv4PrefixAndMask(network.IP, ones)
		result = append(result, HostIPv4PrefixProbe{
			Interface:  iface,
			Address:    src,
			Prefix:     prefix,
			PrefixLen:  ones,
			SubnetMask: subnet,
			Gateway:    gateway,
			Source:     "route",
		})
	}
	return result
}

func ipv4PrefixAndMask(ip net.IP, prefixLen int) (string, string) {
	v4 := ip.To4()
	if v4 == nil {
		return "", ""
	}
	mask := net.CIDRMask(prefixLen, 32)
	network := v4.Mask(mask)
	subnet := fmt.Sprintf("%d.%d.%d.%d", mask[0], mask[1], mask[2], mask[3])
	return fmt.Sprintf("%s/%d", network.String(), prefixLen), subnet
}

func isPublicIPv4(ip net.IP) bool {
	ip = ip.To4()
	if ip == nil {
		return false
	}
	return !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() && !ip.IsMulticast() && !ip.IsUnspecified()
}

func isContainerLikeInterfaceName(iface string) bool {
	prefixes := []string{"lo", "lxc", "docker", "br-", "veth", "virbr", "cni", "flannel", "cali", "kube", "dummy", "ifb"}
	for _, prefix := range prefixes {
		if iface == prefix || strings.HasPrefix(iface, prefix) {
			return true
		}
	}
	return false
}

func isTunnelLikeInterfaceName(iface string) bool {
	lower := strings.ToLower(strings.TrimSpace(iface))
	for _, prefix := range []string{"tun", "tap", "wg", "gre", "gretap", "sit", "ip6tnl", "he-", "zt", "tailscale"} {
		if lower == prefix || strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

func collectIPv6Addresses(nics []HostNICProbe) []HostIPProbe {
	result := make([]HostIPProbe, 0)
	for _, nic := range nics {
		for _, ip := range nic.IPv6 {
			if ip.Scope == "global" {
				result = append(result, ip)
			}
		}
	}
	return result
}

func detectGateways() []HostGatewayProbe {
	gateways := make([]HostGatewayProbe, 0)
	for _, item := range []struct {
		family string
		args   []string
	}{{"ipv4", []string{"-4", "route", "show", "default"}}, {"ipv6", []string{"-6", "route", "show", "default"}}} {
		out := runCommandOutput(3*time.Second, "ip", item.args...)
		for _, line := range strings.Split(out, "\n") {
			fields := strings.Fields(line)
			gw := ""
			iface := ""
			for i, field := range fields {
				if field == "via" && i+1 < len(fields) {
					gw = fields[i+1]
				}
				if field == "dev" && i+1 < len(fields) {
					iface = fields[i+1]
				}
			}
			if gw != "" || iface != "" {
				gateways = append(gateways, HostGatewayProbe{Family: item.family, Interface: iface, Gateway: gw})
			}
		}
	}
	return gateways
}

func detectGPUs() []HostGPUProbe {
	gpus := make([]HostGPUProbe, 0)
	out := runCommandOutput(3*time.Second, "sh", "-c", "lspci -nnk 2>/dev/null | grep -iEA3 'vga|3d|display' || true")
	var current *HostGPUProbe
	for _, line := range strings.Split(out, "\n") {
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)
		if strings.Contains(lower, "vga") || strings.Contains(lower, "3d controller") || strings.Contains(lower, "display controller") {
			gpus = append(gpus, HostGPUProbe{Name: trimmed, Vendor: detectGPUVendor(trimmed), Type: detectGPUType(trimmed)})
			current = &gpus[len(gpus)-1]
			continue
		}
		if current != nil && strings.HasPrefix(trimmed, "Kernel driver in use:") {
			current.Driver = strings.TrimSpace(strings.TrimPrefix(trimmed, "Kernel driver in use:"))
		}
	}
	return gpus
}

func detectGPUVendor(value string) string {
	lower := strings.ToLower(value)
	switch {
	case strings.Contains(lower, "intel"):
		return "Intel"
	case strings.Contains(lower, "nvidia"):
		return "NVIDIA"
	case strings.Contains(lower, "amd") || strings.Contains(lower, "ati"):
		return "AMD"
	case strings.Contains(lower, "virtio") || strings.Contains(lower, "red hat") || strings.Contains(lower, "qemu"):
		return "Virtio"
	default:
		return "Unknown"
	}
}

func detectGPUType(value string) string {
	lower := strings.ToLower(value)
	if strings.Contains(lower, "virtio") || strings.Contains(lower, "red hat") || strings.Contains(lower, "qemu") {
		return "virtual"
	}
	if strings.Contains(lower, "intel") {
		return "integrated"
	}
	return "discrete"
}

func hasIntegratedGPU(gpus []HostGPUProbe) bool {
	for _, gpu := range gpus {
		if gpu.Type == "integrated" {
			return true
		}
	}
	return false
}

func detectRuntimeProbe(env []HostEnvCheck) HostRuntimeProbe {
	devKVM := fileExists("/dev/kvm")
	nested, detail := detectNestedVirtualization()
	lxcOK := envCheckOK(env, "lxc-create")
	kvmSupportedArch := runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64"
	probe := HostRuntimeProbe{
		LXCAvailable:         lxcOK,
		KVMAvailable:         kvmSupportedArch && devKVM && envCheckOK(env, "virsh") && envCheckOK(env, kvmQEMUCheckKey()),
		DevKVM:               devKVM,
		NestedVirtualization: nested,
		NestedDetail:         detail,
		SupportMode:          "unsupported",
	}
	if probe.KVMAvailable {
		probe.SupportMode = "kvm_lxc"
	} else if probe.LXCAvailable {
		probe.SupportMode = "lxc_only"
	}
	return probe
}

func detectNestedVirtualization() (bool, string) {
	paths := []string{
		"/sys/module/kvm_intel/parameters/nested",
		"/sys/module/kvm_amd/parameters/nested",
	}
	for _, path := range paths {
		value := strings.TrimSpace(readFirstExistingFile(path))
		if value == "" {
			continue
		}
		enabled := strings.EqualFold(value, "Y") || value == "1"
		return enabled, filepath.Base(filepath.Dir(filepath.Dir(path))) + "=" + value
	}
	if fileExists("/dev/kvm") {
		return true, "/dev/kvm present"
	}
	return false, "no kvm nested parameter or /dev/kvm"
}

func detectSystemProbe() HostSystemProbe {
	uptime := int64(0)
	if data, err := os.ReadFile("/proc/uptime"); err == nil {
		first := strings.Fields(string(data))
		if len(first) > 0 {
			value, _ := strconv.ParseFloat(first[0], 64)
			uptime = int64(value)
		}
	}
	return HostSystemProbe{
		UptimeSeconds: uptime,
		UptimeText:    formatDurationText(uptime),
		ProcessCount:  countProcesses(),
	}
}

func detectHostEnvironment() []HostEnvCheck {
	qemuCheck := commandCheck(kvmQEMUCheckKey(), "QEMU/KVM 虚拟机", false, kvmQEMUCommand(), "")
	checks := []HostEnvCheck{
		commandCheck("service-manager", "服务管理器 systemd/OpenRC", true, "systemctl", "systemd"),
		commandCheck("lxc-create", "LXC 创建工具", true, "lxc-create", ""),
		commandCheck("lxc-start", "LXC 启动工具", true, "lxc-start", ""),
		commandCheck("iptables", "iptables 网络规则", true, "iptables", ""),
		commandCheck("ip", "iproute2 网络工具", true, "ip", ""),
		commandCheck("conntrack", "conntrack NAT 连接清理", false, "conntrack", ""),
		commandCheck("virsh", "libvirt virsh", false, "virsh", ""),
		qemuCheck,
		commandCheck("genisoimage", "KVM cloud-init ISO 工具", false, "genisoimage", "xorriso/mkisofs 可替代"),
		commandCheck("xorriso", "ISO 备用工具", false, "xorriso", ""),
		commandCheck("smartctl", "硬盘健康检测", false, "smartctl", ""),
		certbotCheck(),
	}
	if envCheckOK(checks, "lxc-start") {
		limit, err := lxc.HostInotifyInstances()
		check := HostEnvCheck{Key: lxc.InotifyInstancesKey, Label: lxc.InotifyInstancesKey, Detail: "—"}
		if err == nil {
			check.OK = limit >= lxc.MinInotifyInstances
			check.Detail = fmt.Sprintf("%d / ≥ %d", limit, lxc.MinInotifyInstances)
		}
		checks = append(checks, check)
	}
	checks = append(checks, HostEnvCheck{Key: "dev-kvm", Label: "/dev/kvm 硬件虚拟化", OK: fileExists("/dev/kvm"), Required: false, Detail: boolDetail(fileExists("/dev/kvm"))})
	checks = append(checks, HostEnvCheck{Key: "ipv4-forward", Label: "IPv4 转发", OK: strings.TrimSpace(readFirstExistingFile("/proc/sys/net/ipv4/ip_forward")) == "1", Required: true, Detail: strings.TrimSpace(readFirstExistingFile("/proc/sys/net/ipv4/ip_forward"))})
	checks = append(checks, HostEnvCheck{Key: "lxcfs", Label: "lxcfs 服务", OK: serviceActive("lxcfs"), Required: false, Detail: serviceDetail("lxcfs")})
	checks = append(checks, HostEnvCheck{Key: "libvirt", Label: "libvirt 服务", OK: serviceActive("libvirtd") || serviceActive("virtqemud"), Required: false, Detail: firstNonEmpty(serviceDetail("libvirtd"), serviceDetail("virtqemud"))})
	return checks
}

func kvmQEMUCheckKey() string {
	switch runtime.GOARCH {
	case "arm64":
		return "qemu-system-aarch64"
	default:
		return "qemu-system-x86_64"
	}
}

func kvmQEMUCommand() string {
	return kvmQEMUCheckKey()
}

func commandCheck(key, label string, required bool, cmd string, fallback string) HostEnvCheck {
	ok := commandExists(cmd)
	detail := "missing"
	if ok {
		detail = commandVersionDetail(cmd)
		if detail == "" {
			detail = "installed"
		}
	} else if fallback != "" {
		detail = fallback
	}
	return HostEnvCheck{Key: key, Label: label, OK: ok, Required: required, Detail: detail}
}

func commandVersionDetail(cmd string) string {
	switch cmd {
	case "ip":
		return strings.TrimSpace(runCommandOutput(2*time.Second, "sh", "-c", "ip -V 2>&1 | head -n 1"))
	default:
		return strings.TrimSpace(runCommandOutput(2*time.Second, "sh", "-c", cmd+" --version 2>&1 | head -n 1"))
	}
}

func certbotCheck() HostEnvCheck {
	check := HostEnvCheck{Key: "certbot", Label: "Certbot 证书工具 >= 5.4", Required: false, Detail: "missing"}
	if !commandExists("certbot") {
		return check
	}
	detail := strings.TrimSpace(runCommandOutput(3*time.Second, "certbot", "--version"))
	if detail == "" {
		detail = strings.TrimSpace(runCommandOutput(3*time.Second, "sh", "-c", "certbot --version 2>&1 | head -n 1"))
	}
	if detail == "" {
		detail = "installed, version unknown"
	}
	check.Detail = detail
	version := extractCertbotVersion(detail)
	check.OK = certbotVersionAtLeast(version, 5, 4)
	if version == "" {
		check.Detail = detail + " (version unknown, need >= 5.4)"
	} else if !check.OK {
		check.Detail = detail + " (need >= 5.4)"
	}
	return check
}

func extractCertbotVersion(output string) string {
	for _, field := range strings.Fields(output) {
		field = strings.Trim(field, "vV,;:()[]{}")
		if field == "" || field[0] < '0' || field[0] > '9' {
			continue
		}
		return field
	}
	return ""
}

func certbotVersionAtLeast(version string, minMajor, minMinor int) bool {
	parts := strings.Split(version, ".")
	if len(parts) < 2 {
		return false
	}
	major, err := strconv.Atoi(numericPrefix(parts[0]))
	if err != nil {
		return false
	}
	minor, err := strconv.Atoi(numericPrefix(parts[1]))
	if err != nil {
		return false
	}
	if major != minMajor {
		return major > minMajor
	}
	return minor >= minMinor
}

func numericPrefix(value string) string {
	var b strings.Builder
	for _, r := range value {
		if r < '0' || r > '9' {
			break
		}
		b.WriteRune(r)
	}
	return b.String()
}

func envCheckOK(checks []HostEnvCheck, key string) bool {
	for _, check := range checks {
		if check.Key == key {
			return check.OK
		}
	}
	return false
}

func serviceActive(name string) bool {
	if commandExists("systemctl") {
		return strings.TrimSpace(runCommandOutput(2*time.Second, "systemctl", "is-active", name)) == "active"
	}
	if commandExists("rc-service") {
		return strings.Contains(runCommandOutput(2*time.Second, "rc-service", name, "status"), "started")
	}
	return false
}

func serviceDetail(name string) string {
	if commandExists("systemctl") {
		return strings.TrimSpace(runCommandOutput(2*time.Second, "systemctl", "is-active", name))
	}
	if commandExists("rc-service") {
		return strings.TrimSpace(runCommandOutput(2*time.Second, "rc-service", name, "status"))
	}
	return "unknown"
}

func readKeyValueFile(path, sep string) map[string]string {
	result := map[string]string{}
	data, err := os.ReadFile(path)
	if err != nil {
		return result
	}
	for _, line := range strings.Split(string(data), "\n") {
		parts := strings.SplitN(line, sep, 2)
		if len(parts) == 2 {
			result[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}
	return result
}

func readFirstExistingFile(paths ...string) string {
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err == nil {
			return string(data)
		}
	}
	return ""
}

func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func runCommandOutput(timeout time.Duration, name string, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil && len(out) == 0 {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func shortCommandDetail(value string) string {
	value = strings.TrimSpace(value)
	lines := strings.Split(value, "\n")
	if len(lines) > 4 {
		lines = lines[:4]
	}
	return strings.Join(lines, " | ")
}

func shellQuoteSimple(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func formatDurationText(seconds int64) string {
	days := seconds / 86400
	seconds %= 86400
	hours := seconds / 3600
	seconds %= 3600
	minutes := seconds / 60
	if days > 0 {
		return fmt.Sprintf("%dd %dh %dm", days, hours, minutes)
	}
	return fmt.Sprintf("%dh %dm", hours, minutes)
}

func formatBytesText(value uint64) string {
	if value == 0 {
		return "0 B"
	}
	units := []string{"B", "KB", "MB", "GB", "TB", "PB"}
	next := float64(value)
	index := 0
	for next >= 1024 && index < len(units)-1 {
		next /= 1024
		index++
	}
	if index == 0 {
		return fmt.Sprintf("%d %s", value, units[index])
	}
	return fmt.Sprintf("%.1f %s", next, units[index])
}

func countProcesses() int {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0
	}
	count := 0
	for _, entry := range entries {
		if _, err := strconv.Atoi(entry.Name()); err == nil {
			count++
		}
	}
	return count
}

func boolDetail(ok bool) string {
	if ok {
		return "available"
	}
	return "missing"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && value != "inactive" && value != "unknown" {
			return value
		}
	}
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
