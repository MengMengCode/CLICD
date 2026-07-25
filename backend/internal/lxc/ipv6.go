package lxc

import (
	"context"
	"encoding/binary"
	"fmt"
	"math/big"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"clicd/internal/config"
)

const ipv6GatewayLinkLocal = "fe80::1"
const MaxPublicIPAssignments = 64

type IPv6PrefixInfo struct {
	Interface string `json:"interface"`
	Address   string `json:"address"`
	Prefix    string `json:"prefix"`
	PrefixLen int    `json:"prefix_len"`
	Gateway   string `json:"gateway"`
	IsTunnel  bool   `json:"is_tunnel"`
	Source    string `json:"source"`
}

type PublicIPInfo struct {
	Address    string `json:"address"`
	Interface  string `json:"interface"`
	Prefix     string `json:"prefix"`
	PrefixLen  int    `json:"prefix_len,omitempty"`
	SubnetMask string `json:"subnet_mask,omitempty"`
	Gateway    string `json:"gateway,omitempty"`
	IsTunnel   bool   `json:"is_tunnel"`
	Source     string `json:"source"`
}

type PublicIPv4ScanResult struct {
	PublicIPInfo
	Status string `json:"status"`
	Usable bool   `json:"usable"`
	Reason string `json:"reason"`
}

type IPv6Status struct {
	Available bool             `json:"available"`
	Reachable bool             `json:"reachable"`
	Reason    string           `json:"reason"`
	Prefixes  []IPv6PrefixInfo `json:"prefixes"`
}

func (m *Manager) DetectIPv6Status() IPv6Status {
	status := IPv6Status{}
	prefixes := DetectPublicIPv6Prefixes()
	status.Prefixes = prefixes
	if len(prefixes) == 0 {
		status.Reason = "no usable public IPv6 prefix found; /128 single-address IPv6 is not assignable"
		return status
	}
	status.Reachable = ipv6ConnectivityOK()
	if !status.Reachable {
		status.Reason = "host has an IPv6 prefix, but outbound IPv6 connectivity test failed"
		return status
	}
	status.Available = true
	status.Reason = "usable public IPv6 prefix detected"
	return status
}

func DetectPublicIPv6Prefixes() []IPv6PrefixInfo {
	if configured := ConfiguredPublicIPv6Prefixes(); len(configured) > 0 {
		return configured
	}
	return detectPublicIPv6Prefixes(detectIPv6DefaultRoutes())
}

func DetectHostPublicIPv6Prefixes() []IPv6PrefixInfo {
	return detectPublicIPv6Prefixes(detectIPv6DefaultRoutes())
}

func ConfiguredPublicIPv6Prefixes() []IPv6PrefixInfo {
	if config.AppConfig == nil || len(config.AppConfig.PublicIPv6Prefixes) == 0 {
		return nil
	}
	detected := detectPublicIPv6Prefixes(detectIPv6DefaultRoutes())
	defaultIface := ""
	defaultGateway := ""
	for _, item := range detected {
		if defaultIface == "" {
			defaultIface = item.Interface
		}
		if defaultGateway == "" {
			defaultGateway = item.Gateway
		}
	}
	result := make([]IPv6PrefixInfo, 0, len(config.AppConfig.PublicIPv6Prefixes))
	for _, item := range config.AppConfig.PublicIPv6Prefixes {
		address := strings.TrimSpace(item.Address)
		prefixText := strings.TrimSpace(item.Prefix)
		if prefixText == "" && address != "" && item.PrefixLen > 0 {
			prefixText = address + "/" + strconv.Itoa(item.PrefixLen)
		}
		prefix, err := netip.ParsePrefix(prefixText)
		if err != nil || !prefix.Addr().Is6() {
			continue
		}
		iface := strings.TrimSpace(item.Interface)
		if iface == "" {
			iface = defaultIface
		}
		gateway := strings.TrimSpace(item.Gateway)
		if gateway == "" {
			gateway = defaultGateway
		}
		addr := strings.TrimSpace(item.Address)
		if addr == "" {
			addr = prefix.Addr().String()
		}
		result = append(result, IPv6PrefixInfo{
			Interface: iface,
			Address:   addr,
			Prefix:    prefix.Masked().String(),
			PrefixLen: prefix.Bits(),
			Gateway:   gateway,
			IsTunnel:  isTunnelLikeInterface(iface),
			Source:    "manual",
		})
	}
	sort.SliceStable(result, func(i, j int) bool {
		return result[i].Prefix < result[j].Prefix
	})
	return result
}

func DetectPublicIPv4() PublicIPInfo {
	addresses := detectPublicIPv4LocalAddresses()
	if len(addresses) == 0 {
		return PublicIPInfo{}
	}
	return addresses[0]
}

func DetectPublicIPv4Candidates() []PublicIPInfo {
	return ConfiguredPublicIPv4Pool()
}

func DetectFreePublicIPv4Candidates(skipContainerID int) []PublicIPInfo {
	used := assignedPublicIPv4Map(skipContainerID)
	candidates := DetectPublicIPv4Candidates()
	result := make([]PublicIPInfo, 0, len(candidates))
	for _, item := range candidates {
		if item.Address == "" || used[item.Address] {
			continue
		}
		result = append(result, item)
	}
	return result
}

func ConfiguredPublicIPv4Pool() []PublicIPInfo {
	if config.AppConfig == nil {
		return nil
	}
	localAddrs := detectPublicIPv4LocalAddresses()
	primaryHostIP := ""
	defaultIface := ""
	defaultPrefixLen := 32
	defaultRoutes := detectIPv4DefaultRoutes()
	gateways := ipv4GatewaysByInterface(defaultRoutes)
	if len(localAddrs) > 0 {
		primaryHostIP = localAddrs[0].Address
		defaultIface = localAddrs[0].Interface
		if localAddrs[0].PrefixLen > 0 {
			defaultPrefixLen = localAddrs[0].PrefixLen
		}
	}

	type candidate struct {
		info  PublicIPInfo
		score int
	}
	candidatesByAddr := map[string]candidate{}
	addCandidate := func(info PublicIPInfo, score int) {
		if info.Address == "" || info.Address == primaryHostIP || (info.Gateway != "" && info.Address == info.Gateway) {
			return
		}
		if existing, ok := candidatesByAddr[info.Address]; ok && existing.score >= score {
			return
		}
		candidatesByAddr[info.Address] = candidate{info: info, score: score}
	}

	defaultIfaces := map[string]bool{}
	for _, route := range defaultRoutes {
		defaultIfaces[route.Interface] = true
	}
	for _, item := range config.AppConfig.PublicIPv4Pool {
		address := strings.TrimSpace(item.Address)
		if address == "" {
			continue
		}
		addr, err := netip.ParseAddr(address)
		if err != nil || !addr.Is4() {
			continue
		}
		address = addr.String()
		iface := strings.TrimSpace(item.Interface)
		gateway := strings.TrimSpace(item.Gateway)
		if iface == "" && gateway != "" {
			iface = ipv4InterfaceForGateway(gateway, defaultRoutes)
		}
		if iface == "" {
			iface = defaultIface
		}
		if gateway == "" && iface != "" {
			gateway = gateways[iface]
		}
		prefixLen := item.PrefixLen
		if prefixLen <= 0 || prefixLen > 32 {
			if gatewayAddr, err := netip.ParseAddr(gateway); err == nil && gatewayAddr.Is4() {
				prefixLen = inferPublicIPv4PrefixLen(addr, gatewayAddr, iface, localAddrs)
			}
			if prefixLen <= 0 || prefixLen > 32 {
				prefixLen = defaultPrefixLen
			}
		}
		score := publicInterfaceScore(iface, defaultIfaces)
		addCandidate(PublicIPInfo{
			Address:    address,
			Interface:  iface,
			Prefix:     ipv4PrefixString(addr, prefixLen),
			PrefixLen:  prefixLen,
			SubnetMask: ipv4SubnetMask(prefixLen),
			Gateway:    gateway,
			IsTunnel:   isTunnelLikeInterface(iface),
			Source:     "manual",
		}, score+50)
	}

	candidates := make([]candidate, 0, len(candidatesByAddr))
	for _, item := range candidatesByAddr {
		candidates = append(candidates, item)
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].score == candidates[j].score {
			return compareIPv4Strings(candidates[i].info.Address, candidates[j].info.Address) < 0
		}
		return candidates[i].score > candidates[j].score
	})
	result := make([]PublicIPInfo, 0, len(candidates))
	for _, c := range candidates {
		result = append(result, c.info)
	}
	return result
}

func NormalizePublicIPv4Pool(items []config.PublicIPv4Assignment) ([]config.PublicIPv4Assignment, error) {
	localAddrs := detectPublicIPv4LocalAddresses()
	primaryHostIP := ""
	defaultIface := ""
	defaultRoutes := detectIPv4DefaultRoutes()
	gateways := ipv4GatewaysByInterface(defaultRoutes)
	localMap := map[string]bool{}
	if len(localAddrs) > 0 {
		primaryHostIP = localAddrs[0].Address
		defaultIface = localAddrs[0].Interface
	}
	for _, local := range localAddrs {
		localMap[local.Address] = true
	}
	assigned := assignedPublicIPv4Map(0)

	seen := map[string]bool{}
	result := make([]config.PublicIPv4Assignment, 0, len(items))
	for _, item := range items {
		raw := strings.TrimSpace(item.Address)
		if raw == "" {
			continue
		}
		gateway := strings.TrimSpace(item.Gateway)
		if gateway == "" && item.Interface != "" {
			gateway = gateways[strings.TrimSpace(item.Interface)]
		}
		if gateway == "" {
			return nil, fmt.Errorf("gateway is required for IPv4 %s", raw)
		}
		gatewayAddr, err := netip.ParseAddr(gateway)
		if err != nil || !gatewayAddr.Is4() {
			return nil, fmt.Errorf("gateway %s is not a valid IPv4 address", gateway)
		}
		gateway = gatewayAddr.String()

		iface := strings.TrimSpace(item.Interface)
		if iface == "" {
			iface = ipv4InterfaceForGateway(gateway, defaultRoutes)
		}
		if iface == "" {
			iface = defaultIface
		}
		if iface == "" {
			return nil, fmt.Errorf("interface is required for IPv4 %s", raw)
		}

		addresses, prefixLen, singleInput, err := expandPublicIPv4Input(raw, item.PrefixLen, gatewayAddr, iface, localAddrs)
		if err != nil {
			return nil, err
		}
		addedFromInput := 0
		skippedReasons := []string{}
		for _, addr := range addresses {
			if !addr.Is4() || !isPublicIPv4(addr) {
				if singleInput {
					return nil, fmt.Errorf("IPv4 %s is not a valid public IPv4 address", addr.String())
				}
				continue
			}
			address := addr.String()
			switch {
			case address == primaryHostIP:
				if singleInput {
					return nil, fmt.Errorf("IPv4 %s is the host primary IPv4 and cannot be allocated", address)
				}
				skippedReasons = append(skippedReasons, address+" is the host primary IPv4")
				continue
			case address == gateway:
				if singleInput {
					return nil, fmt.Errorf("IPv4 %s is the gateway and cannot be allocated", address)
				}
				skippedReasons = append(skippedReasons, address+" is the gateway")
				continue
			case seen[address]:
				continue
			case ipv4AddressResponds(iface, address) && !localMap[address] && !assigned[address]:
				if singleInput {
					return nil, fmt.Errorf("IPv4 %s responds on the network and cannot be allocated", address)
				}
				skippedReasons = append(skippedReasons, address+" responds on the network")
				continue
			}
			seen[address] = true
			result = append(result, config.PublicIPv4Assignment{
				Address:   address,
				Interface: iface,
				PrefixLen: prefixLen,
				Gateway:   gateway,
			})
			addedFromInput++
		}
		if singleInput && addedFromInput == 0 {
			if len(skippedReasons) > 0 {
				return nil, fmt.Errorf("IPv4 %s is not allocatable: %s", raw, strings.Join(skippedReasons, "; "))
			}
			return nil, fmt.Errorf("IPv4 %s is not allocatable", raw)
		}
	}
	if len(result) == 0 && len(items) > 0 {
		return nil, fmt.Errorf("no allocatable public IPv4 address found in the submitted pool")
	}
	sort.SliceStable(result, func(i, j int) bool {
		return compareIPv4Strings(result[i].Address, result[j].Address) < 0
	})
	return result, nil
}

func expandPublicIPv4Input(raw string, requestedPrefixLen int, gateway netip.Addr, iface string, localAddrs []PublicIPInfo) ([]netip.Addr, int, bool, error) {
	if strings.Contains(raw, "/") {
		prefix, err := netip.ParsePrefix(raw)
		if err != nil || !prefix.Addr().Is4() {
			return nil, 0, false, fmt.Errorf("invalid IPv4 segment: %s", raw)
		}
		prefix = prefix.Masked()
		if prefix.Bits() < 24 {
			return nil, 0, false, fmt.Errorf("IPv4 segment %s is too large; use /24 or smaller", prefix.String())
		}
		return ipv4HostsInPrefix(prefix), prefix.Bits(), prefix.Bits() == 32, nil
	}

	addr, err := netip.ParseAddr(raw)
	if err != nil || !addr.Is4() {
		return nil, 0, true, fmt.Errorf("IPv4 %s is not a valid public IPv4 address", raw)
	}
	prefixLen := requestedPrefixLen
	if prefixLen <= 0 || prefixLen > 32 {
		prefixLen = inferPublicIPv4PrefixLen(addr, gateway, iface, localAddrs)
	}
	if prefixLen <= 0 || prefixLen > 32 {
		prefixLen = 32
	}
	return []netip.Addr{addr}, prefixLen, true, nil
}

func ipv4GatewaysByInterface(routes []routeInfo) map[string]string {
	gateways := map[string]string{}
	for _, route := range routes {
		if route.Interface != "" && route.Gateway != "" && gateways[route.Interface] == "" {
			gateways[route.Interface] = route.Gateway
		}
	}
	return gateways
}

func ipv4InterfaceForGateway(gateway string, defaultRoutes []routeInfo) string {
	gateway = strings.TrimSpace(gateway)
	if gateway == "" {
		return ""
	}
	for _, route := range defaultRoutes {
		if route.Gateway == gateway && route.Interface != "" {
			return route.Interface
		}
	}
	out, err := exec.Command("ip", "-4", "route", "get", gateway).Output()
	if err != nil {
		return ""
	}
	fields := strings.Fields(string(out))
	for i := 0; i < len(fields)-1; i++ {
		if fields[i] == "dev" {
			return normalizeIface(fields[i+1])
		}
	}
	return ""
}

func assignedPublicIPv4Map(skipContainerID int) map[string]bool {
	assigned := map[string]bool{}
	if config.AppConfig == nil {
		return assigned
	}
	for _, c := range config.AppConfig.Containers {
		if skipContainerID > 0 && c.ID == skipContainerID {
			continue
		}
		for _, item := range c.PublicIPv4s {
			if address := strings.TrimSpace(item.Address); address != "" {
				assigned[address] = true
			}
		}
	}
	return assigned
}

func inferPublicIPv4PrefixLen(addr netip.Addr, gateway netip.Addr, iface string, localAddrs []PublicIPInfo) int {
	best := 0
	consider := func(prefixText string, prefixIface string) {
		if iface != "" && prefixIface != "" && iface != prefixIface {
			return
		}
		prefix, err := netip.ParsePrefix(prefixText)
		if err != nil || !prefix.Addr().Is4() || !prefix.Contains(addr) {
			return
		}
		if gateway.IsValid() && gateway.Is4() && !prefix.Contains(gateway) {
			return
		}
		if bits := prefix.Bits(); bits > best {
			best = bits
		}
	}
	for _, local := range localAddrs {
		if local.Prefix != "" {
			consider(local.Prefix, local.Interface)
			continue
		}
		if local.Address != "" && local.PrefixLen > 0 {
			consider(local.Address+"/"+strconv.Itoa(local.PrefixLen), local.Interface)
		}
	}

	out, err := exec.Command("ip", "-4", "route", "show").Output()
	if err != nil {
		return best
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] == "default" {
			continue
		}
		if !strings.Contains(fields[0], "/") {
			continue
		}
		routeIface := ""
		for i := 0; i < len(fields)-1; i++ {
			if fields[i] == "dev" {
				routeIface = normalizeIface(fields[i+1])
				break
			}
		}
		consider(fields[0], routeIface)
	}
	return best
}

func ipv4PrefixString(addr netip.Addr, prefixLen int) string {
	if prefixLen <= 0 || prefixLen > 32 {
		prefixLen = 32
	}
	prefix, err := netip.ParsePrefix(addr.String() + "/" + strconv.Itoa(prefixLen))
	if err != nil {
		return addr.String() + "/" + strconv.Itoa(prefixLen)
	}
	return prefix.Masked().String()
}

func ipv4SubnetMask(prefixLen int) string {
	if prefixLen < 0 || prefixLen > 32 {
		return ""
	}
	var mask uint32
	if prefixLen > 0 {
		mask = ^uint32(0) << uint(32-prefixLen)
	}
	return uint32ToIPv4(mask).String()
}

func NormalizePublicIPv6Prefixes(items []config.PublicIPv6Prefix) ([]config.PublicIPv6Prefix, error) {
	detected := detectPublicIPv6Prefixes(detectIPv6DefaultRoutes())
	defaultIface := ""
	defaultGateway := ""
	for _, item := range detected {
		if defaultIface == "" {
			defaultIface = item.Interface
		}
		if defaultGateway == "" {
			defaultGateway = item.Gateway
		}
	}
	seen := map[string]bool{}
	result := make([]config.PublicIPv6Prefix, 0, len(items))
	for _, item := range items {
		raw := strings.TrimSpace(item.Prefix)
		if raw == "" {
			raw = strings.TrimSpace(item.Address)
			if raw != "" && !strings.Contains(raw, "/") && item.PrefixLen > 0 {
				raw += "/" + strconv.Itoa(item.PrefixLen)
			}
		}
		if raw == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(raw)
		if err != nil || !prefix.Addr().Is6() {
			return nil, fmt.Errorf("invalid IPv6 prefix: %s", raw)
		}
		if prefix.Bits() > 120 {
			return nil, fmt.Errorf("IPv6 prefix %s is too small for allocation", prefix.String())
		}
		if !isPublicIPv6(prefix.Addr()) {
			return nil, fmt.Errorf("IPv6 prefix %s is not public", prefix.String())
		}
		key := prefix.Masked().String()
		if seen[key] {
			continue
		}
		iface := strings.TrimSpace(item.Interface)
		if iface == "" {
			iface = defaultIface
		}
		if iface == "" {
			return nil, fmt.Errorf("interface is required for IPv6 prefix %s", key)
		}
		gateway := strings.TrimSpace(item.Gateway)
		if gateway == "" {
			gateway = defaultGateway
		}
		seen[key] = true
		result = append(result, config.PublicIPv6Prefix{
			Address:   prefix.Addr().String(),
			Prefix:    key,
			PrefixLen: prefix.Bits(),
			Interface: iface,
			Gateway:   gateway,
		})
	}
	sort.SliceStable(result, func(i, j int) bool {
		return result[i].Prefix < result[j].Prefix
	})
	return result, nil
}

func ScanPublicIPv4Segment(cidr string, iface string, gateway string, verify bool, limit int) ([]PublicIPv4ScanResult, error) {
	if limit <= 0 || limit > 256 {
		limit = 256
	}
	cidr = strings.TrimSpace(cidr)
	if cidr == "" {
		if host := DetectPublicIPv4(); host.Prefix != "" {
			cidr = host.Prefix
		}
	}
	prefix, err := netip.ParsePrefix(cidr)
	if err != nil || !prefix.Addr().Is4() {
		return nil, fmt.Errorf("invalid IPv4 segment: %s", cidr)
	}
	prefix = prefix.Masked()
	bits := prefix.Bits()
	if bits < 24 {
		return nil, fmt.Errorf("IPv4 segment %s is too large; use /24 or smaller", prefix.String())
	}

	localAddrs := detectPublicIPv4LocalAddresses()
	primaryHostIP := ""
	defaultIface := strings.TrimSpace(iface)
	defaultPrefixLen := bits
	defaultRoutes := detectIPv4DefaultRoutes()
	gateway = strings.TrimSpace(gateway)
	if gateway == "" && defaultIface != "" {
		gateway = ipv4GatewaysByInterface(defaultRoutes)[defaultIface]
	}
	if gateway != "" {
		gatewayAddr, err := netip.ParseAddr(gateway)
		if err != nil || !gatewayAddr.Is4() {
			return nil, fmt.Errorf("gateway %s is not a valid IPv4 address", gateway)
		}
		gateway = gatewayAddr.String()
	}
	if defaultIface == "" && gateway != "" {
		defaultIface = ipv4InterfaceForGateway(gateway, defaultRoutes)
	}
	localMap := map[string]bool{}
	for _, local := range localAddrs {
		if primaryHostIP == "" {
			primaryHostIP = local.Address
		}
		if defaultIface == "" {
			defaultIface = local.Interface
		}
		localMap[local.Address] = true
	}
	if defaultIface == "" {
		return nil, fmt.Errorf("interface is required")
	}
	if gateway == "" {
		return nil, fmt.Errorf("gateway is required")
	}
	assigned := map[string]bool{}
	for _, c := range config.AppConfig.Containers {
		for _, item := range c.PublicIPv4s {
			if item.Address != "" {
				assigned[item.Address] = true
			}
		}
	}
	inPool := map[string]bool{}
	for _, item := range config.AppConfig.PublicIPv4Pool {
		if item.Address != "" {
			inPool[item.Address] = true
		}
	}

	addresses := ipv4HostsInPrefix(prefix)
	if len(addresses) > limit {
		return nil, fmt.Errorf("IPv4 segment %s has %d hosts; limit is %d", prefix.String(), len(addresses), limit)
	}

	results := make([]PublicIPv4ScanResult, 0, len(addresses))
	for _, addr := range addresses {
		address := addr.String()
		info := PublicIPInfo{
			Address:    address,
			Interface:  defaultIface,
			Prefix:     prefix.String(),
			PrefixLen:  defaultPrefixLen,
			SubnetMask: ipv4SubnetMask(defaultPrefixLen),
			Gateway:    gateway,
			IsTunnel:   isTunnelLikeInterface(defaultIface),
			Source:     "scan",
		}
		result := PublicIPv4ScanResult{PublicIPInfo: info, Status: "unknown", Reason: "not checked"}
		switch {
		case address == primaryHostIP:
			result.Status = "host"
			result.Reason = "host primary IPv4"
		case address == gateway:
			result.Status = "gateway"
			result.Reason = "default gateway"
		case assigned[address]:
			result.Status = "assigned"
			result.Usable = true
			result.Reason = "already assigned to a container"
		case inPool[address]:
			result.Status = "pool"
			result.Usable = true
			result.Reason = "already in allocation pool"
		case localMap[address]:
			result.Status = "configured"
			result.Usable = true
			result.Reason = "already configured on host"
		case ipv4AddressResponds(defaultIface, address):
			result.Status = "in_use"
			result.Reason = "address responds on the network"
		case verify:
			if ok, reason := verifyIPv4SourceUsable(defaultIface, address); ok {
				result.Status = "available"
				result.Usable = true
				result.Reason = reason
			} else {
				result.Status = "unknown"
				result.Reason = reason
			}
		default:
			result.Status = "available"
			result.Usable = true
			result.Reason = "no duplicate response detected; source routing not verified"
		}
		results = append(results, result)
	}
	return results, nil
}

func ipv4HostsInPrefix(prefix netip.Prefix) []netip.Addr {
	bits := prefix.Bits()
	base := ipv4ToUint32(prefix.Masked().Addr())
	count := uint64(1) << uint(32-bits)
	if bits == 32 {
		return []netip.Addr{uint32ToIPv4(base)}
	}
	start := uint64(base)
	end := start + count - 1
	if bits <= 30 {
		start++
		end--
	}
	result := make([]netip.Addr, 0, count)
	for value := start; value <= end; value++ {
		result = append(result, uint32ToIPv4(uint32(value)))
	}
	return result
}

func ipv4AddressResponds(iface, address string) bool {
	if commandExists("arping") {
		if exec.Command("arping", "-D", "-I", iface, "-c", "2", "-w", "3", address).Run() != nil {
			return true
		}
		return false
	}
	return exec.Command("ping", "-4", "-I", iface, "-c", "1", "-W", "1", address).Run() == nil
}

func verifyIPv4SourceUsable(iface, address string) (bool, string) {
	cidr := address + "/32"
	added := false
	if exec.Command("ip", "-4", "addr", "show", "dev", iface, "to", cidr).Run() != nil {
		output, err := exec.Command("ip", "-4", "addr", "add", cidr, "dev", iface, "label", iface+":clicdscan").CombinedOutput()
		if err != nil {
			return false, "failed to temporarily bind address: " + strings.TrimSpace(string(output))
		}
		added = true
	}
	if added {
		defer exec.Command("ip", "-4", "addr", "del", cidr, "dev", iface).Run()
	}
	if commandExists("curl") {
		for _, target := range []string{"https://api.ipify.org", "https://ifconfig.me/ip", "http://ifconfig.me/ip"} {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			out, err := exec.CommandContext(ctx, "curl", "-4", "-sS", "--interface", address, "--max-time", "4", target).Output()
			cancel()
			if err == nil && strings.TrimSpace(string(out)) == address {
				return true, "source IPv4 verified by external check"
			}
		}
	}
	for _, target := range []string{"1.1.1.1", "8.8.8.8"} {
		if exec.Command("ping", "-4", "-I", address, "-c", "1", "-W", "2", target).Run() == nil {
			return true, "source IPv4 can reach external network"
		}
	}
	return false, "no duplicate response, but source IPv4 verification failed"
}

func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func detectPublicIPv4LocalAddresses() []PublicIPInfo {
	out, err := exec.Command("ip", "-4", "-o", "addr", "show", "scope", "global").Output()
	if err != nil {
		return nil
	}
	defaultRoutes := detectIPv4DefaultRoutes()
	defaultIfaces := map[string]bool{}
	for _, route := range defaultRoutes {
		defaultIfaces[route.Interface] = true
	}
	type candidate struct {
		info  PublicIPInfo
		score int
	}
	var candidates []candidate
	seen := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 || fields[2] != "inet" {
			continue
		}
		iface := normalizeIface(fields[1])
		if isContainerLikeInterface(iface) {
			continue
		}
		prefix, err := netip.ParsePrefix(fields[3])
		if err != nil || !prefix.Addr().Is4() || !isPublicIPv4(prefix.Addr()) {
			continue
		}
		key := iface + "|" + prefix.Addr().String()
		if seen[key] {
			continue
		}
		seen[key] = true
		score := publicInterfaceScore(iface, defaultIfaces)
		candidates = append(candidates, candidate{
			info: PublicIPInfo{
				Address:    prefix.Addr().String(),
				Interface:  iface,
				Prefix:     prefix.Masked().String(),
				PrefixLen:  prefix.Bits(),
				SubnetMask: ipv4SubnetMask(prefix.Bits()),
				Gateway:    ipv4GatewayForInterface(defaultRoutes, iface),
				IsTunnel:   isTunnelLikeInterface(iface),
				Source:     "local",
			},
			score: score,
		})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})
	result := make([]PublicIPInfo, 0, len(candidates))
	for _, c := range candidates {
		result = append(result, c.info)
	}
	return result
}

func AllocatePublicIPv4Assignments(id int, requested []string, count int, auto bool) ([]config.PublicIPv4Assignment, error) {
	var err error
	count, err = NormalizePublicIPAllocationCount(requested, count)
	if err != nil {
		return nil, err
	}
	candidates := DetectPublicIPv4Candidates()
	if len(candidates) == 0 {
		if len(requested) > 0 || auto {
			return nil, fmt.Errorf("public IPv4 allocation is unavailable: no usable public IPv4 address found")
		}
		return nil, nil
	}
	byAddress := map[string]PublicIPInfo{}
	for _, item := range candidates {
		byAddress[item.Address] = item
	}

	used := map[string]bool{}
	for _, c := range config.AppConfig.Containers {
		if c.ID == id {
			continue
		}
		for _, item := range c.PublicIPv4s {
			if item.Address != "" {
				used[item.Address] = true
			}
		}
	}

	result := []config.PublicIPv4Assignment{}
	selected := map[string]bool{}
	for _, raw := range requested {
		raw = strings.TrimSpace(raw)
		if raw == "" || selected[raw] {
			continue
		}
		addr, err := netip.ParseAddr(raw)
		if err != nil || !addr.Is4() {
			return nil, fmt.Errorf("requested IPv4 %s is not valid", raw)
		}
		raw = addr.String()
		if selected[raw] {
			continue
		}
		info, ok := byAddress[raw]
		if !ok {
			return nil, fmt.Errorf("requested IPv4 %s is not an allocatable public IPv4", raw)
		}
		if used[raw] {
			return nil, fmt.Errorf("requested IPv4 %s is already assigned", raw)
		}
		selected[raw] = true
		used[raw] = true
		result = append(result, config.PublicIPv4Assignment{Address: info.Address, Interface: info.Interface, PrefixLen: info.PrefixLen, Gateway: info.Gateway})
	}

	if len(result) >= count || !auto {
		return result, nil
	}
	for _, info := range candidates {
		if used[info.Address] || selected[info.Address] {
			continue
		}
		used[info.Address] = true
		selected[info.Address] = true
		result = append(result, config.PublicIPv4Assignment{Address: info.Address, Interface: info.Interface, PrefixLen: info.PrefixLen, Gateway: info.Gateway})
		if len(result) >= count {
			return result, nil
		}
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("no free public IPv4 address is available")
	}
	if len(result) < count {
		return nil, fmt.Errorf("only %d free public IPv4 address(es) are available; %d requested", len(result), count)
	}
	return result, nil
}

func NormalizePublicIPAllocationCount(requested []string, count int) (int, error) {
	if len(requested) > MaxPublicIPAssignments {
		return 0, fmt.Errorf("IP address count cannot exceed %d", MaxPublicIPAssignments)
	}
	if count <= 0 {
		count = 1
	}
	if len(requested) > count {
		count = len(requested)
	}
	if count > MaxPublicIPAssignments {
		return 0, fmt.Errorf("IP address count cannot exceed %d", MaxPublicIPAssignments)
	}
	return count, nil
}

func EnsureAssignedPublicIPv4s(assignments []config.PublicIPv4Assignment) {
	for _, assignment := range assignments {
		addr := strings.TrimSpace(assignment.Address)
		iface := strings.TrimSpace(assignment.Interface)
		if addr == "" || iface == "" {
			continue
		}
		prefixLen := assignment.PrefixLen
		if prefixLen <= 0 || prefixLen > 32 {
			if info, ok := publicIPv4InfoByAddress(addr); ok && info.PrefixLen > 0 {
				prefixLen = info.PrefixLen
				if iface == "" {
					iface = info.Interface
				}
			}
		}
		if prefixLen <= 0 || prefixLen > 32 {
			prefixLen = 32
		}
		prefixLen = publicIPv4BindingPrefixLen(assignment, prefixLen)
		cidr := fmt.Sprintf("%s/%d", addr, prefixLen)
		if publicIPv4AddressBound(addr, iface) {
			continue
		}
		if output, err := exec.Command("ip", "-4", "addr", "add", cidr, "dev", iface, "label", iface+":clicd").CombinedOutput(); err != nil {
			fmt.Printf("Warning: failed to add assigned public IPv4 %s to %s: %v, output: %s\n", cidr, iface, err, string(output))
		}
	}
}

func publicIPv4BindingPrefixLen(assignment config.PublicIPv4Assignment, prefixLen int) int {
	if prefixLen <= 0 || prefixLen > 32 {
		return 32
	}
	addr, addrErr := netip.ParseAddr(strings.TrimSpace(assignment.Address))
	gateway, gatewayErr := netip.ParseAddr(strings.TrimSpace(assignment.Gateway))
	if addrErr != nil || gatewayErr != nil || !addr.Is4() || !gateway.Is4() {
		return prefixLen
	}
	prefix, err := netip.ParsePrefix(addr.String() + "/" + strconv.Itoa(prefixLen))
	if err != nil {
		return prefixLen
	}
	if !prefix.Masked().Contains(gateway) {
		return 32
	}
	return prefixLen
}

func publicIPv4AddressBound(address string, iface string) bool {
	out, err := exec.Command("ip", "-4", "-o", "addr", "show", "dev", iface).Output()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 || fields[2] != "inet" {
			continue
		}
		prefix, err := netip.ParsePrefix(fields[3])
		if err == nil && prefix.Addr().String() == address {
			return true
		}
	}
	return false
}

func EnsureAllAssignedPublicIPv4s() {
	for i := range config.AppConfig.Containers {
		EnsureAssignedPublicIPv4s(config.AppConfig.Containers[i].PublicIPv4s)
	}
}

func publicIPv4InfoByAddress(address string) (PublicIPInfo, bool) {
	for _, info := range DetectPublicIPv4Candidates() {
		if info.Address == address {
			return info, true
		}
	}
	return PublicIPInfo{}, false
}

func detectPublicIPv6Prefixes(defaultRoutes []routeInfo) []IPv6PrefixInfo {
	out, err := exec.Command("ip", "-6", "-o", "addr", "show", "scope", "global").Output()
	if err != nil {
		return nil
	}
	defaultIfaces := map[string]bool{}
	gateways := map[string]string{}
	for _, route := range defaultRoutes {
		defaultIfaces[route.Interface] = true
		if route.Gateway != "" && gateways[route.Interface] == "" {
			gateways[route.Interface] = route.Gateway
		}
	}
	type candidate struct {
		info  IPv6PrefixInfo
		score int
	}
	var candidates []candidate
	seen := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 || fields[2] != "inet6" {
			continue
		}
		iface := normalizeIface(fields[1])
		if isContainerLikeInterface(iface) {
			continue
		}
		prefix, err := netip.ParsePrefix(fields[3])
		if err != nil || !prefix.Addr().Is6() {
			continue
		}
		addr := prefix.Addr()
		if !isPublicIPv6(addr) {
			continue
		}
		// Require at least 8 host bits. /128 is a single address, not a usable segment.
		if prefix.Bits() > 120 {
			continue
		}
		masked := prefix.Masked()
		key := iface + "|" + masked.String()
		if seen[key] {
			continue
		}
		seen[key] = true
		score := publicInterfaceScore(iface, defaultIfaces)
		if masked.Bits() <= 64 {
			score += 20
		}
		info := IPv6PrefixInfo{
			Interface: iface,
			Address:   addr.String(),
			Prefix:    masked.String(),
			PrefixLen: masked.Bits(),
			Gateway:   gateways[iface],
			IsTunnel:  isTunnelLikeInterface(iface),
			Source:    "local",
		}
		candidates = append(candidates, candidate{info: info, score: score})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})
	result := make([]IPv6PrefixInfo, 0, len(candidates))
	for _, c := range candidates {
		result = append(result, c.info)
	}
	return result
}

type routeInfo struct {
	Interface string
	Gateway   string
	Metric    int
}

func detectIPv4DefaultRoutes() []routeInfo {
	out, err := exec.Command("ip", "-4", "route", "show", "default").Output()
	if err != nil {
		return nil
	}
	return parseDefaultRoutes(string(out))
}

func ipv4GatewayForInterface(routes []routeInfo, iface string) string {
	for _, route := range routes {
		if route.Interface == iface && route.Gateway != "" {
			return route.Gateway
		}
	}
	return ""
}

func detectIPv6DefaultRoutes() []routeInfo {
	out, err := exec.Command("ip", "-6", "route", "show", "default").Output()
	if err != nil {
		return nil
	}
	return parseDefaultRoutes(string(out))
}

func parseDefaultRoutes(output string) []routeInfo {
	var routes []routeInfo
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] != "default" {
			continue
		}
		route := routeInfo{Metric: 1024}
		for i := 1; i < len(fields)-1; i++ {
			switch fields[i] {
			case "dev":
				route.Interface = normalizeIface(fields[i+1])
			case "via":
				route.Gateway = fields[i+1]
			case "metric":
				metric, err := strconv.Atoi(fields[i+1])
				if err == nil {
					route.Metric = metric
				}
			}
		}
		if route.Interface != "" {
			routes = append(routes, route)
		}
	}
	sort.SliceStable(routes, func(i, j int) bool {
		return routes[i].Metric < routes[j].Metric
	})
	return routes
}

func ipv6ConnectivityOK() bool {
	targets := [][]string{
		{"ping", "-6", "-c", "1", "-W", "2", "2606:4700:4700::1111"},
		{"ping", "-6", "-c", "1", "-W", "2", "2001:4860:4860::8888"},
		{"ping6", "-c", "1", "-W", "2", "2606:4700:4700::1111"},
	}
	for _, args := range targets {
		if exec.Command(args[0], args[1:]...).Run() == nil {
			return true
		}
	}
	return false
}

func isContainerLikeInterface(iface string) bool {
	prefixes := []string{
		"lo", "lxc", "docker", "br-", "veth", "virbr", "cni", "flannel", "cali",
		"kube", "dummy", "ifb", "zt", "zerotier",
	}
	for _, prefix := range prefixes {
		if iface == prefix || strings.HasPrefix(iface, prefix) {
			return true
		}
	}
	return false
}

func normalizeIface(iface string) string {
	iface = strings.TrimSuffix(iface, ":")
	if at := strings.Index(iface, "@"); at >= 0 {
		iface = iface[:at]
	}
	return iface
}

func publicInterfaceScore(iface string, defaultIfaces map[string]bool) int {
	score := 0
	if defaultIfaces[iface] {
		score += 100
	}
	if isTunnelLikeInterface(iface) {
		score -= 120
	} else {
		score += 80
	}
	if isLikelyPhysicalInterface(iface) {
		score += 40
	}
	if operState(iface) == "up" {
		score += 10
	}
	return score
}

func isLikelyPhysicalInterface(iface string) bool {
	prefixes := []string{"eth", "ens", "eno", "enp", "em", "bond", "team"}
	for _, prefix := range prefixes {
		if strings.HasPrefix(iface, prefix) {
			return true
		}
	}
	return false
}

func isTunnelLikeInterface(iface string) bool {
	lower := strings.ToLower(iface)
	prefixes := []string{
		"wg", "wgcf", "warp", "cloudflare", "tun", "tap", "tailscale", "ts",
		"vpn", "ppp", "ipsec", "gre", "gretap", "sit", "he-", "nebula", "zt",
	}
	for _, prefix := range prefixes {
		if lower == prefix || strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return strings.Contains(lower, "warp") || strings.Contains(lower, "cloudflare")
}

func operState(iface string) string {
	path, err := safeSysClassNetFile(iface, "operstate")
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func safeSysClassNetFile(iface string, filename string) (string, error) {
	iface = strings.TrimSpace(iface)
	filename = strings.TrimSpace(filename)
	if iface == "" || filename == "" || len(iface) > 64 || len(filename) > 64 {
		return "", fmt.Errorf("invalid sysfs network path")
	}
	if iface == "." || iface == ".." || filename == "." || filename == ".." {
		return "", fmt.Errorf("invalid sysfs network path")
	}
	if strings.ContainsAny(iface, "/\\\x00") || strings.ContainsAny(filename, "/\\\x00") {
		return "", fmt.Errorf("invalid sysfs network path")
	}
	base := filepath.Clean("/sys/class/net")
	path := filepath.Clean(filepath.Join(base, iface, filename))
	if !strings.HasPrefix(path, base+string(os.PathSeparator)) {
		return "", fmt.Errorf("invalid sysfs network path")
	}
	return path, nil
}

func isPublicIPv4(addr netip.Addr) bool {
	if !addr.IsGlobalUnicast() || addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast() {
		return false
	}
	raw := addr.As4()
	if raw[0] == 100 && raw[1] >= 64 && raw[1] <= 127 {
		return false
	}
	if raw[0] == 192 && raw[1] == 0 && raw[2] == 0 {
		return false
	}
	if raw[0] == 192 && raw[1] == 0 && raw[2] == 2 {
		return false
	}
	if raw[0] == 198 && (raw[1] == 18 || raw[1] == 19 || raw[1] == 51 && raw[2] == 100) {
		return false
	}
	if raw[0] == 203 && raw[1] == 0 && raw[2] == 113 {
		return false
	}
	return true
}

func ipv4ToUint32(addr netip.Addr) uint32 {
	raw := addr.As4()
	return binary.BigEndian.Uint32(raw[:])
}

func uint32ToIPv4(value uint32) netip.Addr {
	var raw [4]byte
	binary.BigEndian.PutUint32(raw[:], value)
	return netip.AddrFrom4(raw)
}

func compareIPv4Strings(a, b string) int {
	addrA, errA := netip.ParseAddr(a)
	addrB, errB := netip.ParseAddr(b)
	if errA == nil && errB == nil && addrA.Is4() && addrB.Is4() {
		rawA := addrA.As4()
		rawB := addrB.As4()
		for i := range rawA {
			if rawA[i] < rawB[i] {
				return -1
			}
			if rawA[i] > rawB[i] {
				return 1
			}
		}
		return 0
	}
	return strings.Compare(a, b)
}

func isPublicIPv6(addr netip.Addr) bool {
	if !addr.IsGlobalUnicast() || addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast() {
		return false
	}
	return !strings.HasPrefix(addr.String(), "2001:db8:")
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
	count, err = NormalizePublicIPAllocationCount(requested, count)
	if err != nil {
		return nil, err
	}
	status := m.DetectIPv6Status()
	if !status.Available {
		return nil, fmt.Errorf("public IPv6 allocation is unavailable: %s", status.Reason)
	}
	prefixes := make([]struct {
		info   IPv6PrefixInfo
		prefix netip.Prefix
	}, 0, len(status.Prefixes))
	for _, prefixInfo := range status.Prefixes {
		prefix, err := netip.ParsePrefix(prefixInfo.Prefix)
		if err != nil {
			continue
		}
		prefixes = append(prefixes, struct {
			info   IPv6PrefixInfo
			prefix netip.Prefix
		}{info: prefixInfo, prefix: prefix})
	}
	if len(prefixes) == 0 {
		return nil, fmt.Errorf("public IPv6 allocation is unavailable: no valid IPv6 prefix")
	}

	used := map[string]bool{}
	hostAddrs := map[string]bool{}
	for _, p := range status.Prefixes {
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
		for i, item := range prefixes {
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
		result = append(result, config.IPv6Assignment{Address: raw, PrefixLen: prefixes[matchIndex].prefix.Bits(), Interface: prefixes[matchIndex].info.Interface})
	}

	if len(result) >= count || !auto {
		return result, nil
	}

	for _, item := range prefixes {
		for offset := uint64(0x1000 + id); offset < 0x100000; offset++ {
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

func ipv6AssignmentAddresses(assignments []config.IPv6Assignment) []string {
	values := make([]string, 0, len(assignments))
	for _, item := range assignments {
		if strings.TrimSpace(item.Address) != "" {
			values = append(values, strings.TrimSpace(item.Address))
		}
	}
	return values
}

func normalizeIPv6List(values []string) []string {
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

func shellQuotedIPv6List(values []string) string {
	values = normalizeIPv6List(values)
	if len(values) == 0 {
		return "''"
	}
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		quoted = append(quoted, shellQuote(value))
	}
	return strings.Join(quoted, " ")
}

func (m *Manager) AssignIPv6(id int) (*config.Container, error) {
	c := config.FindContainer(id)
	if c == nil {
		return nil, fmt.Errorf("container not found: %d", id)
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
	if err := m.applyIPv6Config(c.LxcName(), c.IPv6AddressStrings()...); err != nil {
		return nil, err
	}
	rootfsPath := filepath.Join(m.LxcPath, c.LxcName(), "rootfs")
	if _, err := os.Stat(rootfsPath); err == nil {
		if err := installContainerIPv6Init(rootfsPath, c.IPv6AddressStrings()...); err != nil {
			fmt.Printf("Warning: failed to install IPv6 init in %s: %v\n", c.LxcName(), err)
		}
	}
	if err := m.ApplyIPv6(id); err != nil {
		return nil, err
	}
	return c, nil
}

func (m *Manager) UpdateIPv6Assignments(id int, requested []string, count int, auto bool) (*config.Container, error) {
	c := config.FindContainer(id)
	if c == nil {
		return nil, fmt.Errorf("container not found: %d", id)
	}

	oldAssignments := append([]config.IPv6Assignment(nil), c.IPv6Addresses...)
	oldPrimary := c.IPv6
	oldPrimaryPrefixLen := c.IPv6PrefixLen
	oldPrimaryInterface := c.IPv6Interface

	assignments := []config.IPv6Assignment{}
	if auto || len(requested) > 0 {
		allocated, err := m.allocateIPv6AssignmentsForContainer(id, requested, count, auto)
		if err != nil {
			return nil, err
		}
		assignments = allocated
	}

	for _, assignment := range oldAssignments {
		uplink := assignment.Interface
		if uplink == "" {
			uplink = oldPrimaryInterface
		}
		removeHostIPv6Routing(assignment.Address, uplink)
	}
	if len(oldAssignments) == 0 && oldPrimary != "" {
		removeHostIPv6Routing(oldPrimary, oldPrimaryInterface)
		oldAssignments = append(oldAssignments, config.IPv6Assignment{Address: oldPrimary, PrefixLen: oldPrimaryPrefixLen, Interface: oldPrimaryInterface})
	}

	c.IPv6 = ""
	c.IPv6PrefixLen = 0
	c.IPv6Interface = ""
	c.IPv6Addresses = assignments
	c.NormalizeNetworkAssignments()
	config.SaveConfig()

	if err := m.applyIPv6Config(c.LxcName(), c.IPv6AddressStrings()...); err != nil {
		return nil, err
	}
	rootfsPath := filepath.Join(m.LxcPath, c.LxcName(), "rootfs")
	if _, err := os.Stat(rootfsPath); err == nil {
		if len(c.IPv6Addresses) == 0 {
			if err := removeContainerIPv6Init(rootfsPath); err != nil {
				fmt.Printf("Warning: failed to remove IPv6 init in %s: %v\n", c.LxcName(), err)
			}
		} else if err := installContainerIPv6Init(rootfsPath, c.IPv6AddressStrings()...); err != nil {
			fmt.Printf("Warning: failed to install IPv6 init in %s: %v\n", c.LxcName(), err)
		}
	}
	status, _ := m.GetContainerStatus(c.LxcName())
	if status == "running" {
		m.removeGuestIPv6Addresses(c.LxcName(), oldAssignments)
	}
	if len(c.IPv6Addresses) > 0 {
		if err := m.ApplyIPv6(id); err != nil {
			return nil, err
		}
	} else if status == "running" {
		m.removeGuestIPv6DefaultRoute(c.LxcName())
		if err := ApplyFirewallRules(c.ID); err != nil {
			fmt.Printf("Warning: failed to re-apply firewall rules after IPv6 removal for %s: %v\n", c.Name, err)
		}
	}
	return c, nil
}

func (m *Manager) applyIPv6Config(lxcName string, ipv6s ...string) error {
	configFile := filepath.Join(m.LxcPath, lxcName, "config")
	data, err := os.ReadFile(configFile)
	if err != nil {
		return fmt.Errorf("failed to read container config: %v", err)
	}
	lines := strings.Split(string(data), "\n")
	next := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "# clicd managed: public IPv6") ||
			strings.HasPrefix(trimmed, "lxc.net.0.ipv6.address") ||
			strings.HasPrefix(trimmed, "lxc.net.0.ipv6.gateway") {
			continue
		}
		next = append(next, line)
	}
	ipv6s = normalizeIPv6List(ipv6s)
	if len(ipv6s) > 0 {
		next = append(next, "", "# clicd managed: public IPv6 routed /128")
		for _, ipv6 := range ipv6s {
			next = append(next, fmt.Sprintf("lxc.net.0.ipv6.address = %s/128", ipv6))
		}
		next = append(next, "lxc.net.0.ipv6.gateway = auto")
	}
	return os.WriteFile(configFile, []byte(strings.Join(next, "\n")), 0644)
}

func (m *Manager) ApplyIPv6(id int) error {
	c := config.FindContainer(id)
	if c == nil {
		return fmt.Errorf("container not found: %d", id)
	}
	if c.IPv6 == "" && len(c.IPv6Addresses) == 0 {
		return nil
	}
	c.NormalizeNetworkAssignments()
	if c.IPv6Interface == "" {
		status := m.DetectIPv6Status()
		if len(status.Prefixes) == 0 {
			return fmt.Errorf("failed to detect IPv6 uplink for %s", c.IPv6)
		}
		c.IPv6Interface = status.Prefixes[0].Interface
		c.IPv6PrefixLen = status.Prefixes[0].PrefixLen
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

	rootfsPath := filepath.Join(m.LxcPath, c.LxcName(), "rootfs")
	if _, err := os.Stat(rootfsPath); err == nil {
		if err := installContainerIPv6Init(rootfsPath, c.IPv6AddressStrings()...); err != nil {
			fmt.Printf("Warning: failed to install IPv6 init in %s: %v\n", c.LxcName(), err)
		}
	}
	for _, assignment := range c.IPv6Addresses {
		uplink := assignment.Interface
		if uplink == "" {
			uplink = c.IPv6Interface
		}
		if err := ensureHostIPv6Routing(assignment.Address, uplink); err != nil {
			return err
		}
	}
	status, _ := m.GetContainerStatus(c.LxcName())
	if status != "running" {
		return nil
	}
	addrs := shellQuotedIPv6List(c.IPv6AddressStrings())
	cmd := exec.Command("lxc-attach", "-n", c.LxcName(), "--", "sh", "-c",
		fmt.Sprintf("for ip in %s; do ip -6 addr replace \"$ip/128\" dev eth0; done && ip -6 route replace default via %s dev eth0 metric 100",
			addrs, shellQuote(ipv6GatewayLinkLocal)))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to apply IPv6 inside container: %v, output: %s", err, string(output))
	}
	for _, assignment := range c.IPv6Addresses {
		uplink := assignment.Interface
		if uplink == "" {
			uplink = c.IPv6Interface
		}
		removeIPv6NAT66(assignment.Address, uplink)
	}
	if !containerIPv6ConnectivityOK(c.LxcName()) {
		for _, assignment := range c.IPv6Addresses {
			uplink := assignment.Interface
			if uplink == "" {
				uplink = c.IPv6Interface
			}
			ensureIPv6NAT66(assignment.Address, uplink)
		}
	}
	if err := ApplyFirewallRules(c.ID); err != nil {
		fmt.Printf("Warning: failed to re-apply firewall rules after IPv6 setup for %s: %v\n", c.Name, err)
	}
	return nil
}

func ensureHostIPv6Routing(ipv6, uplink string) error {
	if uplink == "" {
		return fmt.Errorf("missing IPv6 uplink interface")
	}
	runQuiet("sysctl", "-w", "net.ipv6.conf.all.forwarding=1")
	runQuiet("sysctl", "-w", "net.ipv6.conf."+uplink+".accept_ra=2")
	runQuiet("sysctl", "-w", "net.ipv6.conf."+uplink+".proxy_ndp=1")
	runQuiet("ip", "link", "set", "lxcbr0", "up")
	runQuiet("ip", "-6", "addr", "add", ipv6GatewayLinkLocal+"/64", "dev", "lxcbr0")
	if out, err := exec.Command("ip", "-6", "route", "replace", ipv6+"/128", "dev", "lxcbr0").CombinedOutput(); err != nil {
		return fmt.Errorf("failed to add IPv6 host route: %v, output: %s", err, string(out))
	}
	if out, err := exec.Command("ip", "-6", "neigh", "replace", "proxy", ipv6, "dev", uplink).CombinedOutput(); err != nil {
		return fmt.Errorf("failed to add IPv6 proxy NDP: %v, output: %s", err, string(out))
	}
	ensureIPv6ForwardRules(ipv6)
	return nil
}

func installContainerIPv6Init(rootfsPath string, ipv6s ...string) error {
	ipv6s = normalizeIPv6List(ipv6s)
	if len(ipv6s) == 0 {
		return nil
	}
	for _, ipv6 := range ipv6s {
		if _, err := netip.ParseAddr(ipv6); err != nil {
			return fmt.Errorf("invalid IPv6 address %q: %w", ipv6, err)
		}
	}

	scriptPath := filepath.Join(rootfsPath, "usr", "local", "sbin", "clicd-ipv6-init")
	if err := os.MkdirAll(filepath.Dir(scriptPath), 0755); err != nil {
		return err
	}
	script := `#!/bin/sh
IPV6_ADDRS="` + strings.Join(ipv6s, " ") + `"
IPV6_GW=` + shellQuote(ipv6GatewayLinkLocal) + `
IFACE="${CLICD_IPV6_IFACE:-eth0}"

command -v ip >/dev/null 2>&1 || exit 0

i=0
while [ "$i" -lt 30 ]; do
	if ip link show dev "$IFACE" >/dev/null 2>&1; then
		break
	fi
	i=$((i + 1))
	sleep 1
done

ip link set dev "$IFACE" up >/dev/null 2>&1 || true
for IPV6_ADDR in $IPV6_ADDRS; do
	ip -6 addr replace "$IPV6_ADDR/128" dev "$IFACE" >/dev/null 2>&1 || true
done
ip -6 route replace default via "$IPV6_GW" dev "$IFACE" metric 100 >/dev/null 2>&1 || true
exit 0
`
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		return err
	}

	osRelease := ""
	if data, err := os.ReadFile(filepath.Join(rootfsPath, "etc", "os-release")); err == nil {
		osRelease = strings.ToLower(string(data))
	}
	hasSystemd := dirExists(filepath.Join(rootfsPath, "etc", "systemd", "system"))
	hasOpenRC := fileExists(filepath.Join(rootfsPath, "sbin", "openrc-run")) || strings.Contains(osRelease, "alpine")

	if hasSystemd {
		if err := installContainerIPv6Systemd(rootfsPath); err != nil {
			return err
		}
	}
	if hasOpenRC {
		if err := installContainerIPv6OpenRC(rootfsPath); err != nil {
			return err
		}
	}
	if !hasSystemd && !hasOpenRC {
		if err := installContainerIPv6SysV(rootfsPath); err != nil {
			return err
		}
	}
	return nil
}

func removeContainerIPv6Init(rootfsPath string) error {
	paths := []string{
		filepath.Join(rootfsPath, "usr", "local", "sbin", "clicd-ipv6-init"),
		filepath.Join(rootfsPath, "etc", "systemd", "system", "clicd-ipv6.service"),
		filepath.Join(rootfsPath, "etc", "systemd", "system", "multi-user.target.wants", "clicd-ipv6.service"),
		filepath.Join(rootfsPath, "etc", "init.d", "clicd-ipv6"),
		filepath.Join(rootfsPath, "etc", "runlevels", "default", "clicd-ipv6"),
	}
	for _, level := range []string{"2", "3", "4", "5"} {
		paths = append(paths, filepath.Join(rootfsPath, "etc", "rc"+level+".d", "S99clicd-ipv6"))
	}
	for _, path := range paths {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func installContainerIPv6Systemd(rootfsPath string) error {
	servicePath := filepath.Join(rootfsPath, "etc", "systemd", "system", "clicd-ipv6.service")
	if err := os.MkdirAll(filepath.Dir(servicePath), 0755); err != nil {
		return err
	}
	service := `[Unit]
Description=CLICD IPv6 setup
After=network-online.target network.target
Wants=network-online.target

[Service]
Type=oneshot
ExecStart=/usr/local/sbin/clicd-ipv6-init
RemainAfterExit=yes

[Install]
WantedBy=multi-user.target
`
	if err := os.WriteFile(servicePath, []byte(service), 0644); err != nil {
		return err
	}
	wantsDir := filepath.Join(rootfsPath, "etc", "systemd", "system", "multi-user.target.wants")
	if err := os.MkdirAll(wantsDir, 0755); err != nil {
		return err
	}
	return replaceSymlink("../clicd-ipv6.service", filepath.Join(wantsDir, "clicd-ipv6.service"))
}

func installContainerIPv6OpenRC(rootfsPath string) error {
	initPath := filepath.Join(rootfsPath, "etc", "init.d", "clicd-ipv6")
	if err := os.MkdirAll(filepath.Dir(initPath), 0755); err != nil {
		return err
	}
	initScript := `#!/sbin/openrc-run
name="CLICD IPv6 setup"
description="Apply CLICD IPv6 settings"

depend() {
	after net networking
	need net
}

start() {
	ebegin "Applying CLICD IPv6"
	/usr/local/sbin/clicd-ipv6-init
	eend $?
}
`
	if err := os.WriteFile(initPath, []byte(initScript), 0755); err != nil {
		return err
	}
	runlevelDir := filepath.Join(rootfsPath, "etc", "runlevels", "default")
	if err := os.MkdirAll(runlevelDir, 0755); err != nil {
		return err
	}
	return replaceSymlink(filepath.Join("..", "..", "init.d", "clicd-ipv6"), filepath.Join(runlevelDir, "clicd-ipv6"))
}

func installContainerIPv6SysV(rootfsPath string) error {
	initDir := filepath.Join(rootfsPath, "etc", "init.d")
	if err := os.MkdirAll(initDir, 0755); err != nil {
		return err
	}
	initPath := filepath.Join(initDir, "clicd-ipv6")
	initScript := `#!/bin/sh
### BEGIN INIT INFO
# Provides:          clicd-ipv6
# Required-Start:    $network
# Required-Stop:
# Default-Start:     2 3 4 5
# Default-Stop:
# Short-Description: CLICD IPv6 setup
### END INIT INFO

case "$1" in
	start|restart|force-reload)
		/usr/local/sbin/clicd-ipv6-init
		;;
	stop|status)
		exit 0
		;;
	*)
		echo "Usage: $0 {start|stop|restart|force-reload|status}"
		exit 1
		;;
esac
exit 0
`
	if err := os.WriteFile(initPath, []byte(initScript), 0755); err != nil {
		return err
	}
	for _, level := range []string{"2", "3", "4", "5"} {
		rcDir := filepath.Join(rootfsPath, "etc", "rc"+level+".d")
		if !dirExists(rcDir) {
			continue
		}
		if err := replaceSymlink(filepath.Join("..", "init.d", "clicd-ipv6"), filepath.Join(rcDir, "S99clicd-ipv6")); err != nil {
			return err
		}
	}
	return nil
}

func replaceSymlink(target, linkPath string) error {
	if current, err := os.Readlink(linkPath); err == nil && current == target {
		return nil
	}
	if err := os.Remove(linkPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Symlink(target, linkPath)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func ensureIPv6ForwardRules(ipv6 string) {
	rules := [][]string{
		{"FORWARD", "-i", "lxcbr0", "-s", ipv6 + "/128", "-j", "ACCEPT"},
		{"FORWARD", "-o", "lxcbr0", "-d", ipv6 + "/128", "-j", "ACCEPT"},
	}
	for _, rule := range rules {
		check := append([]string{"-C"}, rule...)
		add := append([]string{"-A"}, rule...)
		if exec.Command("ip6tables", check...).Run() != nil {
			exec.Command("ip6tables", add...).Run()
		}
	}
}

func removeHostIPv6Routing(ipv6, uplink string) {
	removeIPv6NAT66(ipv6, uplink)
	removeIPv6ForwardRules(ipv6)
	runQuiet("ip", "-6", "route", "del", ipv6+"/128", "dev", "lxcbr0")
	if uplink != "" {
		runQuiet("ip", "-6", "neigh", "del", "proxy", ipv6, "dev", uplink)
	}
}

func removeIPv6ForwardRules(ipv6 string) {
	rules := [][]string{
		{"FORWARD", "-i", "lxcbr0", "-s", ipv6 + "/128", "-j", "ACCEPT"},
		{"FORWARD", "-o", "lxcbr0", "-d", ipv6 + "/128", "-j", "ACCEPT"},
	}
	for _, rule := range rules {
		del := append([]string{"-D"}, rule...)
		for exec.Command("ip6tables", del...).Run() == nil {
		}
	}
}

func containerIPv6ConnectivityOK(lxcName string) bool {
	targets := []string{"2606:4700:4700::1111", "2001:4860:4860::8888"}
	for _, target := range targets {
		if exec.Command("lxc-attach", "-n", lxcName, "--", "ping", "-6", "-c", "1", "-W", "2", target).Run() == nil {
			return true
		}
	}
	return false
}

func (m *Manager) removeGuestIPv6Addresses(lxcName string, assignments []config.IPv6Assignment) {
	addrs := ipv6AssignmentAddresses(assignments)
	if len(addrs) == 0 {
		return
	}
	quoted := shellQuotedIPv6List(addrs)
	_ = exec.Command("lxc-attach", "-n", lxcName, "--", "sh", "-c",
		fmt.Sprintf("for ip in %s; do ip -6 addr del \"$ip/128\" dev eth0 2>/dev/null || true; done", quoted)).Run()
}

func (m *Manager) removeGuestIPv6DefaultRoute(lxcName string) {
	_ = exec.Command("lxc-attach", "-n", lxcName, "--", "sh", "-c",
		fmt.Sprintf("ip -6 route del default via %s dev eth0 2>/dev/null || true", shellQuote(ipv6GatewayLinkLocal))).Run()
}

func ensureIPv6NAT66(ipv6, uplink string) {
	if ipv6 == "" || uplink == "" {
		return
	}
	rule := []string{"POSTROUTING", "-s", ipv6 + "/128", "-o", uplink, "-j", "MASQUERADE"}
	check := append([]string{"-t", "nat", "-C"}, rule...)
	add := append([]string{"-t", "nat", "-A"}, rule...)
	if exec.Command("ip6tables", check...).Run() != nil {
		exec.Command("ip6tables", add...).Run()
	}
}

func removeIPv6NAT66(ipv6, uplink string) {
	if ipv6 == "" || uplink == "" {
		return
	}
	rule := []string{"POSTROUTING", "-s", ipv6 + "/128", "-o", uplink, "-j", "MASQUERADE"}
	del := append([]string{"-t", "nat", "-D"}, rule...)
	for exec.Command("ip6tables", del...).Run() == nil {
	}
}

func runQuiet(name string, args ...string) {
	_ = exec.Command(name, args...).Run()
}

func (m *Manager) AssignedIPv6Count() int {
	count := 0
	for _, c := range config.AppConfig.Containers {
		if strings.TrimSpace(c.IPv6) != "" {
			count++
		}
	}
	return count
}

func IPv6PrefixCapacity(prefixLen int) string {
	if prefixLen <= 0 || prefixLen > 128 {
		return "0"
	}
	hostBits := 128 - prefixLen
	if hostBits > 32 {
		return "large"
	}
	return strconv.FormatUint(uint64(1)<<uint(hostBits), 10)
}
