package api

import (
	"encoding/json"
	"net/http"
	"net/netip"
	"sort"
	"strconv"

	"clicd/internal/config"
	"clicd/internal/lxc"
)

type routeCapacity struct {
	Used      int    `json:"used"`
	Remaining string `json:"remaining"`
	Total     string `json:"total"`
}

type nat4PortRange struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

type nat4Networks struct {
	LXC config.NATNetwork `json:"lxc"`
	KVM config.NATNetwork `json:"kvm"`
}

type nat4Route struct {
	ContainerID   int    `json:"container_id"`
	ContainerName string `json:"container_name"`
	LXCName       string `json:"lxc_name"`
	Status        string `json:"status"`
	IP            string `json:"ip"`
	HostIP        string `json:"host_ip"`
	HostPort      int    `json:"host_port"`
	ContainerPort int    `json:"container_port"`
	Protocol      string `json:"protocol"`
	Description   string `json:"description"`
}

type ipv4Route struct {
	ContainerID   int    `json:"container_id"`
	ContainerName string `json:"container_name"`
	LXCName       string `json:"lxc_name"`
	Status        string `json:"status"`
	Address       string `json:"address"`
	Interface     string `json:"interface"`
	PrefixLen     int    `json:"prefix_len,omitempty"`
	Gateway       string `json:"gateway,omitempty"`
}

type lanDHCPRoute struct {
	ContainerID   int    `json:"container_id"`
	ContainerName string `json:"container_name"`
	LXCName       string `json:"lxc_name"`
	Status        string `json:"status"`
	Address       string `json:"address"`
	Interface     string `json:"interface"`
	PrefixLen     int    `json:"prefix_len,omitempty"`
	Gateway       string `json:"gateway,omitempty"`
	MACAddress    string `json:"mac_address,omitempty"`
	Mode          string `json:"mode"`
}

type ipv6Route struct {
	ContainerID   int    `json:"container_id"`
	ContainerName string `json:"container_name"`
	LXCName       string `json:"lxc_name"`
	Status        string `json:"status"`
	Address       string `json:"address"`
	PrefixLen     int    `json:"prefix_len"`
	Interface     string `json:"interface"`
}

type routingResponse struct {
	NAT4                routeCapacity        `json:"nat4"`
	NAT4PortRange       nat4PortRange        `json:"nat4_port_range"`
	NAT4NextPort        int                  `json:"nat4_next_port"`
	NAT4Networks        nat4Networks         `json:"nat4_networks"`
	IPv4                routeCapacity        `json:"ipv4"`
	LANDHCP             routeCapacity        `json:"lan_dhcp"`
	IPv6                routeCapacity        `json:"ipv6"`
	HostPublicIPv4      lxc.PublicIPInfo     `json:"host_public_ipv4"`
	PublicIPv4Addresses []lxc.PublicIPInfo   `json:"public_ipv4_addresses"`
	IPv4Assignments     []ipv4Route          `json:"ipv4_assignments"`
	LANDHCPAssignments  []lanDHCPRoute       `json:"lan_dhcp_assignments"`
	NAT4Mappings        []nat4Route          `json:"nat4_mappings"`
	IPv6Assignments     []ipv6Route          `json:"ipv6_assignments"`
	IPv6Prefixes        []lxc.IPv6PrefixInfo `json:"ipv6_prefixes"`
}

type routingPoolsRequest struct {
	Addresses     *[]string                      `json:"addresses"`
	Items         *[]config.PublicIPv4Assignment `json:"items"`
	IPv6Prefixes  *[]config.PublicIPv6Prefix     `json:"ipv6_prefixes"`
	NAT4PortRange *nat4PortRange                 `json:"nat4_port_range"`
}

type publicIPv4ScanRequest struct {
	CIDR      string `json:"cidr"`
	Interface string `json:"interface"`
	Gateway   string `json:"gateway"`
	Verify    bool   `json:"verify"`
	Limit     int    `json:"limit"`
}

func HandleRouting(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		handleRoutingGet(w, r)
	case http.MethodPut:
		handleRoutingPoolsUpdate(w, r)
	default:
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
	}
}

func HandleRoutingIPv4Scan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "Method not allowed"})
		return
	}
	if !requireScope(w, r, "routing:write") {
		return
	}
	var req publicIPv4ScanRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid request body"})
		return
	}
	results, err := lxc.ScanPublicIPv4Segment(req.CIDR, req.Interface, req.Gateway, req.Verify, req.Limit)
	if err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: err.Error()})
		return
	}
	jsonResponse(w, http.StatusOK, APIResponse{Success: true, Data: results})
}

func handleRoutingGet(w http.ResponseWriter, r *http.Request) {
	if !hasAnyScope(r, "routing:read", "routing:write") {
		jsonResponse(w, http.StatusForbidden, APIResponse{Success: false, Message: "Insufficient API key scope"})
		return
	}

	nat4Mappings := make([]nat4Route, 0)
	usedPorts := map[int]bool{}
	ipv4Assignments := make([]ipv4Route, 0)
	lanDHCPAssignments := make([]lanDHCPRoute, 0)
	ipv6Assignments := make([]ipv6Route, 0)

	nat4StartPort, nat4EndPort := config.NATPortRange()

	for i := range config.AppConfig.Containers {
		c := &config.AppConfig.Containers[i]
		for _, pm := range c.PortMappings {
			if config.NATPortInRange(pm.HostPort) {
				usedPorts[pm.HostPort] = true
			}
			nat4Mappings = append(nat4Mappings, nat4Route{
				ContainerID:   c.ID,
				ContainerName: c.Name,
				LXCName:       c.LxcName(),
				Status:        c.Status,
				IP:            c.IP,
				HostIP:        pm.HostIP,
				HostPort:      pm.HostPort,
				ContainerPort: pm.ContainerPort,
				Protocol:      pm.Protocol,
				Description:   pm.Description,
			})
		}
		for _, ip := range c.PublicIPv4s {
			if ip.Address == "" {
				continue
			}
			ipv4Assignments = append(ipv4Assignments, ipv4Route{
				ContainerID:   c.ID,
				ContainerName: c.Name,
				LXCName:       c.LxcName(),
				Status:        c.Status,
				Address:       ip.Address,
				Interface:     ip.Interface,
				PrefixLen:     ip.PrefixLen,
				Gateway:       ip.Gateway,
			})
		}
		c.NormalizeNetworkAssignments()
		if c.UsesLANIPv4() {
			lanDHCPAssignments = append(lanDHCPAssignments, lanDHCPRoute{
				ContainerID:   c.ID,
				ContainerName: c.Name,
				LXCName:       c.LxcName(),
				Status:        c.Status,
				Address:       c.IP,
				Interface:     c.LANInterface,
				PrefixLen:     c.LANIPv4PrefixLen,
				Gateway:       c.LANIPv4Gateway,
				MACAddress:    c.MACAddress,
				Mode:          c.LANIPv4Mode,
			})
		}
		for _, ip := range c.IPv6Addresses {
			if ip.Address == "" {
				continue
			}
			ipv6Assignments = append(ipv6Assignments, ipv6Route{
				ContainerID:   c.ID,
				ContainerName: c.Name,
				LXCName:       c.LxcName(),
				Status:        c.Status,
				Address:       ip.Address,
				PrefixLen:     ip.PrefixLen,
				Interface:     ip.Interface,
			})
		}
	}
	sort.SliceStable(nat4Mappings, func(i, j int) bool {
		if nat4Mappings[i].HostPort == nat4Mappings[j].HostPort {
			if nat4Mappings[i].HostIP != nat4Mappings[j].HostIP {
				return nat4Mappings[i].HostIP < nat4Mappings[j].HostIP
			}
			return nat4Mappings[i].ContainerName < nat4Mappings[j].ContainerName
		}
		return nat4Mappings[i].HostPort < nat4Mappings[j].HostPort
	})
	sort.SliceStable(ipv4Assignments, func(i, j int) bool {
		return ipv4Assignments[i].Address < ipv4Assignments[j].Address
	})
	sort.SliceStable(lanDHCPAssignments, func(i, j int) bool {
		if lanDHCPAssignments[i].Interface == lanDHCPAssignments[j].Interface {
			return lanDHCPAssignments[i].ContainerName < lanDHCPAssignments[j].ContainerName
		}
		return lanDHCPAssignments[i].Interface < lanDHCPAssignments[j].Interface
	})
	sort.SliceStable(ipv6Assignments, func(i, j int) bool {
		return ipv6Assignments[i].Address < ipv6Assignments[j].Address
	})

	totalNAT4Ports := config.NATPortCapacity()
	nat4Used := len(usedPorts)
	nat4Remaining := totalNAT4Ports - nat4Used
	if nat4Remaining < 0 {
		nat4Remaining = 0
	}
	nat4NextPort, _ := config.PreviewSSHPortExcluding(nil)

	prefixes := lxc.DetectPublicIPv6Prefixes()
	hostPublicIPv4 := lxc.DetectPublicIPv4()
	publicIPv4s := lxc.DetectPublicIPv4Candidates()
	ipv4Total := len(publicIPv4s)
	ipv4Used := len(ipv4Assignments)
	ipv4Remaining := ipv4Total - ipv4Used
	if ipv4Remaining < 0 {
		ipv4Remaining = 0
	}
	ipv6Total := totalIPv6Capacity(prefixes)
	ipv6Remaining := subtractCapacity(ipv6Total, len(ipv6Assignments))

	jsonResponse(w, http.StatusOK, APIResponse{
		Success: true,
		Data: routingResponse{
			NAT4: routeCapacity{
				Used:      nat4Used,
				Remaining: strconv.Itoa(nat4Remaining),
				Total:     strconv.Itoa(totalNAT4Ports),
			},
			NAT4PortRange: nat4PortRange{
				Start: nat4StartPort,
				End:   nat4EndPort,
			},
			NAT4NextPort: nat4NextPort,
			NAT4Networks: nat4Networks{
				LXC: config.LXCNATNetwork(),
				KVM: config.KVMNATNetwork(),
			},
			IPv4: routeCapacity{
				Used:      ipv4Used,
				Remaining: strconv.Itoa(ipv4Remaining),
				Total:     strconv.Itoa(ipv4Total),
			},
			LANDHCP: routeCapacity{
				Used:      len(lanDHCPAssignments),
				Remaining: "DHCP",
				Total:     "DHCP",
			},
			IPv6: routeCapacity{
				Used:      len(ipv6Assignments),
				Remaining: ipv6Remaining,
				Total:     ipv6Total,
			},
			HostPublicIPv4:      hostPublicIPv4,
			PublicIPv4Addresses: publicIPv4s,
			IPv4Assignments:     ipv4Assignments,
			LANDHCPAssignments:  lanDHCPAssignments,
			NAT4Mappings:        nat4Mappings,
			IPv6Assignments:     ipv6Assignments,
			IPv6Prefixes:        prefixes,
		},
	})
}

func handleRoutingPoolsUpdate(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, "routing:write") {
		return
	}
	var req routingPoolsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Invalid request body"})
		return
	}

	if req.NAT4PortRange != nil {
		start, end, err := config.NormalizeNATPortRange(req.NAT4PortRange.Start, req.NAT4PortRange.End)
		if err != nil {
			jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: err.Error()})
			return
		}
		config.AppConfig.NATPortStart = start
		config.AppConfig.NATPortEnd = end
		if config.AppConfig.NextSSHPort < start || config.AppConfig.NextSSHPort > end {
			config.AppConfig.NextSSHPort = start
		}
	}

	if req.Items != nil || req.Addresses != nil {
		items := []config.PublicIPv4Assignment{}
		if req.Items != nil {
			items = *req.Items
		} else if req.Addresses != nil {
			items = make([]config.PublicIPv4Assignment, 0, len(*req.Addresses))
			for _, address := range *req.Addresses {
				items = append(items, config.PublicIPv4Assignment{Address: address})
			}
		}
		normalized, err := lxc.NormalizePublicIPv4Pool(items)
		if err != nil {
			jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: err.Error()})
			return
		}
		allowed := map[string]bool{}
		for _, item := range normalized {
			allowed[item.Address] = true
		}
		for _, c := range config.AppConfig.Containers {
			for _, item := range c.PublicIPv4s {
				if item.Address != "" && !allowed[item.Address] {
					jsonResponse(w, http.StatusBadRequest, APIResponse{
						Success: false,
						Message: "IPv4 " + item.Address + " is assigned to container " + c.Name + " and cannot be removed from the pool",
					})
					return
				}
			}
		}
		config.AppConfig.PublicIPv4Pool = normalized
	}

	if req.IPv6Prefixes != nil {
		normalized, err := lxc.NormalizePublicIPv6Prefixes(*req.IPv6Prefixes)
		if err != nil {
			jsonResponse(w, http.StatusBadRequest, APIResponse{Success: false, Message: err.Error()})
			return
		}
		parsedPrefixes := make([]netip.Prefix, 0, len(normalized))
		for _, item := range normalized {
			prefix, err := netip.ParsePrefix(item.Prefix)
			if err == nil {
				parsedPrefixes = append(parsedPrefixes, prefix)
			}
		}
		for _, c := range config.AppConfig.Containers {
			c.NormalizeNetworkAssignments()
			for _, item := range c.IPv6Addresses {
				if item.Address == "" {
					continue
				}
				addr, err := netip.ParseAddr(item.Address)
				if err != nil {
					continue
				}
				contained := false
				for _, prefix := range parsedPrefixes {
					if prefix.Contains(addr) {
						contained = true
						break
					}
				}
				if !contained {
					jsonResponse(w, http.StatusBadRequest, APIResponse{
						Success: false,
						Message: "IPv6 " + item.Address + " is assigned to container " + c.Name + " and cannot be removed from the pool",
					})
					return
				}
			}
		}
		config.AppConfig.PublicIPv6Prefixes = normalized
	}

	if err := config.SaveConfig(); err != nil {
		jsonResponse(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Failed to save configuration"})
		return
	}
	handleRoutingGet(w, r)
}

func totalIPv6Capacity(prefixes []lxc.IPv6PrefixInfo) string {
	if len(prefixes) == 0 {
		return "0"
	}
	var total uint64
	for _, prefix := range prefixes {
		capacity := lxc.IPv6PrefixCapacity(prefix.PrefixLen)
		if capacity == "large" {
			return "large"
		}
		parsed, err := strconv.ParseUint(capacity, 10, 64)
		if err != nil {
			continue
		}
		if ^uint64(0)-total < parsed {
			return "large"
		}
		total += parsed
	}
	if total == 0 {
		return "0"
	}
	return strconv.FormatUint(total, 10)
}

func subtractCapacity(total string, used int) string {
	if total == "" || total == "0" {
		return "0"
	}
	if total == "large" {
		return "large"
	}
	parsed, err := strconv.ParseInt(total, 10, 64)
	if err != nil {
		return total
	}
	remaining := parsed - int64(used)
	if remaining < 0 {
		remaining = 0
	}
	return strconv.FormatInt(remaining, 10)
}
