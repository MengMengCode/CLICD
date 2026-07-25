# API Integration

CLICD remains compatible with legacy `/api` endpoints, so existing integrations do not need to change. New integrations should use `/api/v1`; the list below is all v1, and the recommended container list endpoint is `GET /api/v1/containers`.

## Authentication

API keys can be created and managed from the API Integration page. Requests support either of these headers:

```bash
curl -H "X-API-Key: YOUR_API_KEY" https://panel.example.com/api/v1/containers
```

```bash
curl -H "Authorization: Bearer YOUR_API_KEY" https://panel.example.com/api/v1/dashboard
```

## Response Shape

All APIs use the same response envelope:

```json
{
  "success": true,
  "message": "OK",
  "data": {}
}
```

Integrations should read only the business fields they need. New capabilities are added as optional fields where possible, without requiring existing plugins to rename current fields.

## Creation and Reinstall

Container creation, batch creation, reinstall, and batch reinstall support mixed NAT, public IPv4, IPv6 networking, plus Linux SSH login configuration. Public IPv4/IPv6 pools can be viewed with `GET /api/v1/routing` and updated with `PUT /api/v1/routing`.

Create container example:

```json
{
  "name": "demo-lxc-01",
  "virtualization": "lxc",
  "template_id": "debian-bookworm",
  "vcpu": 1,
  "ram_mb": 512,
  "disk_gb": 10,
  "assign_nat": true,
  "port_mapping_count": 2,
  "assign_ipv4": false,
  "ipv4_count": 1,
  "public_ipv4s": [],
  "assign_ipv6": true,
  "ipv6_count": 1,
  "ipv6_addresses": [],
  "ssh_auth_mode": "auto_password",
  "ssh_password": "",
  "ssh_public_key": "",
  "expires_at": "",
  "network_down_mbps": 100,
  "network_up_mbps": 50,
  "io_read_mbps": 120,
  "io_write_mbps": 80
}
```

Field notes:

| Field | Description |
| --- | --- |
| `assign_nat` | Whether to allocate NAT port mappings. If omitted, default NAT behavior is preserved. |
| `assign_ipv4` | Whether to allocate public IPv4. |
| `ipv4_count` | Number of public IPv4 addresses to allocate automatically. |
| `public_ipv4s` | Explicit public IPv4 address list. |
| `assign_ipv6` | Whether to allocate IPv6. |
| `ipv6_count` | Number of IPv6 addresses to allocate automatically. |
| `ipv6_addresses` | Explicit IPv6 address list. |
| `ssh_auth_mode` | Linux creation supports `auto_password`, `password`, and `key`; reinstall also supports `keep`. |
| `ssh_password` | Custom password for `password` mode. It must be 8-64 characters, include letters and digits, and contain no whitespace. |
| `ssh_public_key` | One-line SSH public key for `key` mode. |
| `network_down_mbps` | Optional container download/downlink bandwidth limit in Mbps. `0` means unlimited. |
| `network_up_mbps` | Optional container upload/uplink bandwidth limit in Mbps. `0` means unlimited. |
| `io_read_mbps` | Optional disk read limit in MB/s. `0` means unlimited. |
| `io_write_mbps` | Optional disk write limit in MB/s. `0` means unlimited. |
| `network_bw_mbps` | Legacy-compatible field. Sets symmetric downlink/uplink bandwidth; new integrations should prefer the split fields. |
| `io_speed_mbps` | Legacy-compatible field. Sets symmetric read/write I/O limits; new integrations should prefer the split fields. |

Reinstall example:

```json
{
  "template_id": "debian-bookworm",
  "ssh_auth_mode": "keep",
  "ssh_password": "",
  "ssh_public_key": ""
}
```

`keep` is only for reinstall and keeps the current SSH password. Windows KVM images ignore Linux SSH public key fields.

## Resource and Traffic Limits

`PUT /api/v1/containers/{id}/resource-limit` supports partial updates. Fields omitted from the request remain unchanged.

```json
{
  "vcpu": 2,
  "ram_mb": 1024,
  "network_down_mbps": 100,
  "network_up_mbps": 50,
  "io_read_mbps": 120,
  "io_write_mbps": 80
}
```

Legacy `network_bw_mbps` and `io_speed_mbps` are still accepted. They mean symmetric downlink/uplink bandwidth and symmetric read/write I/O limits. New integrations should use the split fields to control download/upload and read/write independently.

`PUT /api/v1/containers/{id}/traffic-limit` request body:

```json
{
  "traffic_mode": "total",
  "monthly_traffic_gb": 1024,
  "traffic_in_gb": 0,
  "traffic_out_gb": 0
}
```

| Field | Description |
| --- | --- |
| `traffic_mode` | Traffic limit mode. Common values are `total` for a shared total limit and `split` for separate inbound/outbound limits. |
| `monthly_traffic_gb` | Monthly total traffic quota for `total` mode, in GB. `0` means unlimited. |
| `traffic_in_gb` | Monthly inbound quota for `split` mode, in GB. `0` means unlimited. |
| `traffic_out_gb` | Monthly outbound quota for `split` mode, in GB. `0` means unlimited. |

## Container Firewall

Read container firewall settings with `GET /api/v1/containers/{id}/firewall` and update them with `PUT /api/v1/containers/{id}/firewall`. Updates are applied immediately when the container is running.

Update example:

```json
{
  "enabled": true,
  "default_action": "DROP",
  "rules": [
    {
      "direction": "in",
      "protocol": "tcp",
      "action": "ACCEPT",
      "network": "ipv4",
      "source_ip": "203.0.113.0/24",
      "port": "22,80,443",
      "description": "allow admin and web"
    }
  ]
}
```

| Field | Description |
| --- | --- |
| `enabled` | Whether the container firewall is enabled. |
| `default_action` | Default action: `ACCEPT` or `DROP`. |
| `rules[].id` | Optional. Omit for new rules and the backend will generate one. |
| `rules[].direction` | Direction: `in` or `out`. |
| `rules[].protocol` | Protocol: `tcp`, `udp`, `icmp`, or `all`. |
| `rules[].action` | Action: `ACCEPT` or `DROP`. |
| `rules[].network` | Network type: `ipv4`, `ipv6`, or `all`. |
| `rules[].source_ip` | Optional source IP, CIDR, or address range. |
| `rules[].port` | Optional. Supported only for `tcp`/`udp`; examples: `22`, `80,443`, or `8000-9000`. |
| `rules[].description` | Optional note. |

## API Key Create and Update

`POST /api/v1/api-keys` and `PATCH /api/v1/api-keys/{id}` use the same field shape. `name` is required when creating a key; updates overwrite the fields you send.

```json
{
  "name": "Automation",
  "ip_whitelist": "198.51.100.23,203.0.113.0/24",
  "scopes": ["dashboard:read", "container:read", "container:power"],
  "expires_at": "2026-12-31 23:59:59",
  "disabled": false,
  "container_uuids": ["00000000-0000-4000-8000-000000000005"]
}
```

| Field | Description |
| --- | --- |
| `name` | API key name. Required when creating a key. |
| `ip_whitelist` | Optional allowed source IPs/CIDRs, comma-separated. Empty means no IP restriction. |
| `scopes` | Optional permission scopes. If omitted, the default read-only scopes are used. `*` grants all permissions. |
| `expires_at` | Optional expiration time. Empty means no expiration. |
| `disabled` | Whether this key is disabled. |
| `container_uuids` | Optional container allowlist that limits the key to specific containers. |

## Panel Access Source Policy

Use `GET /api/v1/access-policy` to read the panel source allowlist and `PUT /api/v1/access-policy` to update it. Both endpoints require `admin:access`. The policy covers panel pages, login endpoints, and every API.

```json
{
  "enabled": true,
  "allowed_sources": [
    "203.0.113.10",
    "192.168.1.0/24",
    "2001:db8::/32"
  ],
  "trusted_proxies": [
    "127.0.0.1"
  ]
}
```

Both lists accept IPv4, IPv6, and CIDR values. The backend only uses `X-Forwarded-For`, `X-Real-IP`, or `CF-Connecting-IP` when the direct peer matches `trusted_proxies`, so untrusted clients cannot bypass the policy by spoofing those headers. An enabled policy requires at least one allowed source, and the API rejects changes that exclude the current administrator source. Direct loopback access remains available as a CLI/SSH recovery path.

## Python Example

Fetch containers:

```python
import requests

BASE_URL = "https://panel.example.com"
API_KEY = "YOUR_API_KEY"

session = requests.Session()
session.headers.update({
    "X-API-Key": API_KEY,
    "Content-Type": "application/json",
})

resp = session.get(f"{BASE_URL}/api/v1/containers", timeout=15)
resp.raise_for_status()
print(resp.json())
```

Create a port mapping:

```python
import requests

BASE_URL = "https://panel.example.com"
API_KEY = "YOUR_API_KEY"
CONTAINER_ID = "example-vm"

payload = {
    "protocol": "tcp",
    "host_port": 18080,
    "container_port": 80,
    "description": "web",
}

resp = requests.post(
    f"{BASE_URL}/api/v1/containers/{CONTAINER_ID}/port-mappings",
    headers={"X-API-Key": API_KEY},
    json=payload,
    timeout=15,
)
resp.raise_for_status()
print(resp.json())
```

## Endpoint List

### Overview

| Method | Path | Description |
| --- | --- | --- |
| GET | `/api/v1/dashboard` | Dashboard statistics |
| GET | `/api/v1/host-info` | Host resources |
| GET | `/api/v1/host-report` | Host inspection report |
| GET | `/api/v1/routing` | NAT/IPv4/IPv6 routing |
| PUT | `/api/v1/routing` | Update public IPv4/IPv6 pools |
| POST | `/api/v1/routing/ipv4-scan` | Scan a public IPv4 segment |
| GET | `/api/v1/ipv6/status` | IPv6 status |
| GET | `/api/v1/tasks` | Task queue |
| DELETE | `/api/v1/tasks/{task_id}` | Delete a task |

### Containers

| Method | Path | Description |
| --- | --- | --- |
| GET | `/api/v1/containers` | Container list (recommended) |
| GET | `/api/v1/containers/list` | Compatible GET form for container list |
| POST | `/api/v1/containers/list` | Compatible POST form for container list |
| POST | `/api/v1/containers` | Create container |
| GET | `/api/v1/containers/{id\|uuid\|name}` | Container details |
| POST | `/api/v1/containers/{id}/start` | Start |
| POST | `/api/v1/containers/{id}/stop` | Stop |
| POST | `/api/v1/containers/{id}/restart` | Restart |
| POST | `/api/v1/containers/{id}/reinstall` | Reinstall |
| DELETE | `/api/v1/containers/{id}/delete` | Delete |
| GET | `/api/v1/containers/{id}/usage` | Resource usage |
| GET | `/api/v1/containers/{id}/traffic` | Traffic statistics |
| POST | `/api/v1/containers/{id}/traffic-reset` | Reset traffic |
| PUT | `/api/v1/containers/{id}/traffic-limit` | Update traffic limits |
| PUT | `/api/v1/containers/{id}/resource-limit` | Update resource limits |
| PUT | `/api/v1/containers/{id}/expiry` | Update expiration time |
| POST | `/api/v1/containers/{id}/reset-password` | Reset SSH password |
| POST | `/api/v1/containers/{id}/ipv6` | Assign IPv6 |

### Ports and Snapshots

| Method | Path | Description |
| --- | --- | --- |
| GET | `/api/v1/containers/{id}/random-port` | Random available port; accepts `host_ip` to check a specific host IP |
| POST | `/api/v1/containers/{id}/port-mappings` | Add port mapping |
| PUT | `/api/v1/containers/{id}/port-mappings/{index}` | Update port mapping |
| DELETE | `/api/v1/containers/{id}/port-mappings/{index}` | Delete port mapping |
| GET | `/api/v1/containers/{id}/firewall` | Get container firewall settings |
| PUT | `/api/v1/containers/{id}/firewall` | Update container firewall settings |
| GET | `/api/v1/snapshots` | Snapshot overview |
| GET | `/api/v1/containers/{id}/snapshots` | Container snapshots |
| POST | `/api/v1/containers/{id}/snapshots` | Create snapshot |
| DELETE | `/api/v1/containers/{id}/snapshots/{snapshot_id}` | Delete snapshot |
| POST | `/api/v1/containers/{id}/snapshots/{snapshot_id}/restore` | Restore snapshot |
| POST | `/api/v1/containers/{id}/snapshots/schedule` | Schedule snapshots |
| PUT | `/api/v1/containers/{id}/snapshots/quota` | Snapshot quota |

### Platform Management

| Method | Path | Description |
| --- | --- | --- |
| GET | `/api/v1/templates` | Template list |
| GET | `/api/v1/images` | Image management list |
| GET | `/api/v1/images/enabled` | Enabled and downloaded images; supports `type=lxc\|kvm` |
| POST | `/api/v1/images/custom` | Add a third-party LXC/KVM image source |
| DELETE | `/api/v1/images/custom` | Remove a third-party LXC/KVM image source and cache |
| POST | `/api/v1/images/download` | Download image |
| POST | `/api/v1/images/cancel` | Cancel image download |
| DELETE | `/api/v1/images/delete` | Delete image cache |
| PUT | `/api/v1/images/toggle` | Enable or disable image |
| GET | `/api/v1/security/alerts` | Security alerts |
| POST | `/api/v1/security/check` | Run security check |
| GET | `/api/v1/security/logs?container={name}` | Security connection logs |
| GET | `/api/v1/security/summary` | Security summary |
| GET | `/api/v1/security/settings` | Security settings |
| PUT | `/api/v1/security/settings` | Update security settings |
| GET | `/api/v1/swap` | Swap information |
| POST | `/api/v1/swap` | Adjust Swap |
| GET | `/api/v1/language` | Current panel language |
| POST/PUT | `/api/v1/language` | Update panel language |
| GET | `/api/v1/ssl` | SSL settings (requires admin permission / `admin:access`) |
| PUT | `/api/v1/ssl` | Update SSL settings (requires admin permission / `admin:access`) |
| GET | `/api/v1/webssh-origins` | WebSSH Origin allowlist (requires admin permission / `admin:access`) |
| PUT | `/api/v1/webssh-origins` | Update WebSSH Origin allowlist (requires admin permission / `admin:access`) |
| POST | `/api/v1/batch-create` | Batch create containers |
| POST | `/api/v1/batch-action` | Batch power action, delete, or reinstall |
| POST | `/api/v1/ssh-ticket` | Create WebSSH ticket |
| POST | `/api/v1/vnc-ticket` | Create WebVNC ticket |

### Accounts and Logs

| Method | Path | Description |
| --- | --- | --- |
| POST | `/api/v1/sub-user/create` | Create sub-user link |
| GET | `/api/v1/sub-users` | Sub-user list |
| POST | `/api/v1/sub-users/{id}/rotate-password` | Rotate sub-user password |
| GET | `/api/v1/sub-users/{id}/audit-logs` | Sub-user audit logs |
| GET | `/api/v1/sub-users/{id}/login-logs` | Sub-user login logs |
| GET | `/api/v1/audit-logs` | Audit logs |
| GET | `/api/v1/login-logs` | Login logs |
| GET | `/api/v1/api-keys` | API key list |
| POST | `/api/v1/api-keys` | Create API key |
| PATCH | `/api/v1/api-keys/{id}` | Update API key |
| DELETE | `/api/v1/api-keys/{id}` | Delete API key |

## Response Samples

The samples below are grouped by endpoint path. Resource numbers, task IDs, container IDs, timestamps, IP addresses, and keys will differ in real environments. Passwords, tickets, and API keys are masked.

### Overview

```json
{
  "GET /api/v1/dashboard": {
    "success": true,
    "data": {
      "running": 31,
      "stopped": 0,
      "total_containers": 31
    }
  },
  "GET /api/v1/host-info": {
    "success": true,
    "data": {
      "cpu": { "cores": 8, "usage_pct": 1.16 },
      "ram": { "total_mb": 31825, "used_mb": 1275, "free_mb": 30550 },
      "disk": { "total_gb": 1750.49, "used_gb": 123.98, "free_gb": 1626.51 },
      "network": {
        "public_ipv4": "203.0.113.10",
        "public_ipv4_interface": "eth0",
        "public_ipv6": "2001:db8:100::2",
        "public_ipv6_interface": "eth0"
      },
      "load": { "load1": 0.01, "load5": 0.03, "load15": 0.01 }
    }
  },
  "GET /api/v1/host-report": {
    "success": true,
    "data": {
      "generated_at": "2026-06-12 10:00:00",
      "summary": { "status": "ok", "warnings": 0 },
      "host": { "hostname": "node-1", "kernel": "6.8.0" },
      "resources": { "cpu_cores": 8, "ram_total_mb": 31825, "disk_total_gb": 1750.49 },
      "network": { "public_ipv4": "203.0.113.10", "public_ipv6": "2001:db8:100::2" }
    }
  },
  "GET /api/v1/routing": {
    "success": true,
    "data": {
      "nat4": { "used": 62, "remaining": "45474", "total": "45536" },
      "ipv4": { "used": 1, "remaining": "3", "total": "4" },
      "ipv6": { "used": 31, "remaining": "large", "total": "large" },
      "public_ipv4_addresses": [
        { "address": "203.0.113.10", "interface": "eth0", "prefix_len": 32, "gateway": "203.0.113.1" }
      ],
      "ipv4_assignments": [
        { "container_id": 5, "container_name": "example-vm", "address": "203.0.113.10", "interface": "eth0", "prefix_len": 32, "gateway": "203.0.113.1" }
      ],
      "nat4_mappings": [
        { "container_id": 5, "container_name": "example-vm", "status": "running", "ip": "10.0.0.10", "host_port": 22004, "container_port": 22, "protocol": "tcp" }
      ],
      "ipv6_assignments": [
        { "container_id": 5, "container_name": "example-vm", "address": "2001:db8:100::1005", "prefix_len": 64, "interface": "eth0" }
      ]
    }
  },
  "PUT /api/v1/routing": {
    "success": true,
    "data": {
      "ipv4": { "used": 1, "remaining": "3", "total": "4" },
      "public_ipv4_addresses": [
        { "address": "203.0.113.10", "interface": "eth0", "prefix_len": 32, "gateway": "203.0.113.1" }
      ],
      "ipv6_prefixes": [
        { "interface": "eth0", "address": "2001:db8:100::2", "prefix": "2001:db8:100::/64", "prefix_len": 64, "gateway": "2001:db8:100::1" }
      ]
    }
  },
  "POST /api/v1/routing/ipv4-scan": {
    "success": true,
    "data": [
      { "address": "203.0.113.10", "interface": "eth0", "prefix_len": 32, "gateway": "203.0.113.1", "status": "available", "usable": true, "reason": "" }
    ]
  },
  "GET /api/v1/ipv6/status": {
    "success": true,
    "data": {
      "available": true,
      "reachable": true,
      "reason": "usable public IPv6 prefix detected",
      "prefixes": [
        { "interface": "eth0", "address": "2001:db8:100::2", "prefix": "2001:db8:100::/64", "prefix_len": 64, "gateway": "2001:db8:100::1" }
      ]
    }
  },
  "GET /api/v1/tasks": {
    "success": true,
    "data": []
  },
  "DELETE /api/v1/tasks/{task_id}": {
    "success": true,
    "message": "Task deleted"
  }
}
```

### Containers

```json
{
  "GET /api/v1/containers": {
    "success": true,
    "data": [
      {
        "id": 5,
        "uuid": "00000000-0000-4000-8000-000000000005",
        "name": "example-vm",
        "virtualization": "lxc",
        "template": "debian-bullseye",
        "vcpu": 1,
        "ram_mb": 512,
        "disk_gb": 10,
        "network_down_mbps": 100,
        "network_up_mbps": 50,
        "io_read_mbps": 120,
        "io_write_mbps": 80,
        "status": "running",
        "ip": "10.0.0.10",
        "ipv6": "2001:db8:100::1005",
        "ssh_port": 22004,
        "ssh_password": "***",
        "port_mappings": [
          { "container_port": 22, "host_port": 22004, "protocol": "tcp", "description": "SSH" },
          { "container_port": 20000, "host_port": 20000, "protocol": "tcp", "description": "Port-20000" }
        ]
      }
    ]
  },
  "GET /api/v1/containers/list": {
    "success": true,
    "data": [
      { "id": 5, "uuid": "00000000-0000-4000-8000-000000000005", "name": "example-vm", "status": "running", "ip": "10.0.0.10" }
    ]
  },
  "POST /api/v1/containers/list": {
    "success": true,
    "data": [
      { "id": 5, "uuid": "00000000-0000-4000-8000-000000000005", "name": "example-vm", "status": "running", "ip": "10.0.0.10" }
    ]
  },
  "POST /api/v1/containers": {
    "success": true,
    "message": "Container created successfully"
  },
  "GET /api/v1/containers/{id|uuid|name}": {
    "success": true,
    "data": {
      "id": 5,
      "uuid": "00000000-0000-4000-8000-000000000005",
      "name": "example-vm",
      "status": "running",
      "ip": "10.0.0.10",
      "ipv6": "2001:db8:100::1005",
      "ssh_port": 22004,
      "ssh_password": "***",
      "policy_blocked": false
    }
  },
  "POST /api/v1/containers/{id}/start": {
    "success": true,
    "message": "Task queued",
    "data": { "task_id": "task-10", "container_name": "example-vm", "status": "pending", "action": "start" }
  },
  "POST /api/v1/containers/{id}/stop": {
    "success": true,
    "message": "Task queued",
    "data": { "task_id": "task-10", "container_name": "example-vm", "status": "pending", "action": "stop" }
  },
  "POST /api/v1/containers/{id}/restart": {
    "success": true,
    "message": "Task queued",
    "data": { "task_id": "task-10", "container_name": "example-vm", "status": "pending", "action": "restart" }
  },
  "POST /api/v1/containers/{id}/reinstall": {
    "success": true,
    "message": "Task queued",
    "data": { "task_id": "task-10", "container_name": "example-vm", "status": "pending", "action": "reinstall" }
  },
  "DELETE /api/v1/containers/{id}/delete": {
    "success": true,
    "message": "Task queued",
    "data": { "task_id": "task-10", "container_name": "example-vm", "status": "pending", "action": "delete" }
  },
  "GET /api/v1/containers/{id}/usage": {
    "success": true,
    "data": {
      "cpu_usage_pct": 0,
      "cpu_usage_usec": 3908852,
      "memory_usage_bytes": 29331456,
      "disk_usage_bytes": 515100672,
      "network_rx_bytes": 131232,
      "network_tx_bytes": 16828,
      "load1": 0.1,
      "load5": 0.06,
      "load15": 0.01
    }
  },
  "GET /api/v1/containers/{id}/traffic": {
    "success": true,
    "data": {
      "mode": "total",
      "limit_gb": 1024,
      "in_limit_gb": 0,
      "out_limit_gb": 0,
      "total_used_bytes": 142082,
      "rx_used_bytes": 127212,
      "tx_used_bytes": 14870,
      "used_pct": 0,
      "reset_date": "2026-06"
    }
  },
  "POST /api/v1/containers/{id}/traffic-reset": {
    "success": true,
    "message": "Traffic reset"
  },
  "PUT /api/v1/containers/{id}/traffic-limit": {
    "success": true,
    "message": "Traffic limit updated"
  },
  "PUT /api/v1/containers/{id}/resource-limit": {
    "success": true,
    "message": "Resource limits updated"
  },
  "PUT /api/v1/containers/{id}/expiry": {
    "success": true,
    "message": "Expiry updated"
  },
  "POST /api/v1/containers/{id}/reset-password": {
    "success": true,
    "message": "SSH password reset successfully",
    "data": { "password": "***" }
  },
  "POST /api/v1/containers/{id}/ipv6": {
    "success": true,
    "message": "IPv6 assigned",
    "data": { "id": 5, "name": "example-vm", "ipv6": "2001:db8:100::1005" }
  }
}
```

### Ports and Snapshots

```json
{
  "GET /api/v1/containers/{id}/random-port?host_ip=203.0.113.10": {
    "success": true,
    "data": { "port": 61320 }
  },
  "POST /api/v1/containers/{id}/port-mappings": {
    "success": true,
    "data": [
      { "container_port": 22, "host_port": 22004, "protocol": "tcp", "description": "SSH" },
      { "container_port": 8080, "host_port": 61320, "protocol": "tcp", "description": "HTTP" }
    ]
  },
  "PUT /api/v1/containers/{id}/port-mappings/{index}": {
    "success": true,
    "data": [
      { "container_port": 8081, "host_port": 61320, "protocol": "tcp", "description": "HTTP" }
    ]
  },
  "DELETE /api/v1/containers/{id}/port-mappings/{index}": {
    "success": true,
    "data": []
  },
  "GET /api/v1/containers/{id}/firewall": {
    "success": true,
    "data": {
      "enabled": true,
      "default_action": "DROP",
      "rules": [
        { "id": "a1b2c3d4", "direction": "in", "protocol": "tcp", "action": "ACCEPT", "network": "ipv4", "source_ip": "203.0.113.0/24", "port": "22,80,443", "description": "allow admin and web" }
      ]
    }
  },
  "PUT /api/v1/containers/{id}/firewall": {
    "success": true,
    "message": "Firewall updated",
    "data": { "enabled": true, "default_action": "DROP", "rules": [] }
  },
  "GET /api/v1/snapshots": {
    "success": true,
    "data": null
  },
  "GET /api/v1/containers/{id}/snapshots": {
    "success": true,
    "data": {
      "quota": 1,
      "schedule": { "enabled": false, "interval_hours": 0, "last_run": "", "next_run": "", "time": "", "created_by": "" },
      "snapshots": []
    }
  },
  "POST /api/v1/containers/{id}/snapshots": {
    "success": true,
    "data": {
      "id": "snap-20260608-001",
      "container_id": 5,
      "container_name": "example-vm",
      "created_at": "2026-06-08 16:00:00",
      "created_by": "api:Automation",
      "scheduled": false,
      "size_bytes": 10485760
    }
  },
  "DELETE /api/v1/containers/{id}/snapshots/{snapshot_id}": {
    "success": true,
    "message": "Snapshot deleted"
  },
  "POST /api/v1/containers/{id}/snapshots/{snapshot_id}/restore": {
    "success": true,
    "message": "Snapshot restored"
  },
  "POST /api/v1/containers/{id}/snapshots/schedule": {
    "success": true,
    "data": {
      "container": { "id": 5, "name": "example-vm", "snapshot_schedule_enabled": true, "snapshot_schedule_interval_hours": 24, "snapshot_schedule_time": "03:00" }
    }
  },
  "PUT /api/v1/containers/{id}/snapshots/quota": {
    "success": true,
    "data": {
      "quota": 2,
      "container": { "id": 5, "name": "example-vm", "snapshot_limit": 2 }
    }
  }
}
```

### Platform Management

```json
{
  "GET /api/v1/templates": {
    "success": true,
    "data": [
      { "id": "ubuntu-noble", "name": "Ubuntu 24.04", "distro": "ubuntu", "release": "noble", "arch": "amd64", "description": "Ubuntu 24.04 LTS" },
      { "id": "debian-bookworm", "name": "Debian 12", "distro": "debian", "release": "bookworm", "arch": "amd64", "description": "Debian 12 (Bookworm)" }
    ]
  },
  "GET /api/v1/images": {
    "success": true,
    "data": [
      { "id": "ubuntu-noble", "name": "Ubuntu 24.04", "type": "lxc", "downloaded": true, "enabled": true, "downloading": false, "progress": 0, "size_bytes": 135005452 }
    ]
  },
  "GET /api/v1/images/enabled?type=lxc": {
    "success": true,
    "data": [
      { "id": "ubuntu-noble", "name": "Ubuntu 24.04", "distro": "ubuntu", "release": "noble", "arch": "amd64", "variant": "default", "description": "Ubuntu 24.04 LTS", "type": "lxc" }
    ]
  },
  "POST /api/v1/images/download": {
    "success": true,
    "message": "Already downloaded"
  },
  "POST /api/v1/images/cancel": {
    "success": true,
    "message": "Cancel requested"
  },
  "DELETE /api/v1/images/delete": {
    "success": true,
    "message": "Deleted"
  },
  "PUT /api/v1/images/toggle": {
    "success": true,
    "message": "OK"
  },
  "GET /api/v1/security/alerts": {
    "success": true,
    "data": []
  },
  "POST /api/v1/security/check": {
    "success": true,
    "message": "Security check completed"
  },
  "GET /api/v1/security/logs?container={name}": {
    "success": true,
    "data": []
  },
  "GET /api/v1/security/summary": {
    "success": true,
    "data": { "critical": 0, "high": 0, "medium": 0, "low": 0, "total_alerts": 0 }
  },
  "GET /api/v1/security/settings": {
    "success": true,
    "data": { "auto_shutdown": false }
  },
  "PUT /api/v1/security/settings": {
    "success": true,
    "data": { "auto_shutdown": false }
  },
  "GET /api/v1/swap": {
    "success": true,
    "data": { "total_mb": 16383, "used_mb": 0, "free_mb": 16383, "enabled": true, "swap_file": "/swapfile" }
  },
  "POST /api/v1/swap": {
    "success": true,
    "message": "SWAP adjusted to 16384 MB",
    "data": { "total_mb": 16383, "used_mb": 0, "free_mb": 16383, "enabled": true, "swap_file": "/swapfile" }
  },
  "GET /api/v1/language": {
    "success": true,
    "data": { "language": "zh" }
  },
  "PUT /api/v1/language": {
    "success": true,
    "data": { "language": "en" }
  },
  "GET /api/v1/ssl": {
    "success": true,
    "data": { "enabled": true, "mode": "self-signed", "target": "panel.example.com", "detected_host": "panel.example.com", "needs_restart": false }
  },
  "PUT /api/v1/ssl": {
    "success": true,
    "message": "SSL settings saved",
    "data": { "enabled": true, "mode": "self-signed", "target": "panel.example.com", "needs_restart": true }
  },
  "GET /api/v1/webssh-origins": {
    "success": true,
    "data": { "origins": ["https://panel.example.com"], "current_origin": "https://panel.example.com" }
  },
  "PUT /api/v1/webssh-origins": {
    "success": true,
    "message": "Origin allowlist saved",
    "data": { "origins": ["https://panel.example.com"], "current_origin": "https://panel.example.com" }
  },
  "POST /api/v1/batch-create": {
    "success": true,
    "data": ["task-12"]
  },
  "POST /api/v1/batch-action": {
    "success": true,
    "data": ["task-13"]
  },
  "POST /api/v1/ssh-ticket": {
    "success": true,
    "data": { "ticket": "***60 seconds valid***" }
  },
  "POST /api/v1/vnc-ticket": {
    "success": true,
    "data": { "ticket": "***60 seconds valid***" }
  }
}
```

### Accounts and Logs

```json
{
  "POST /api/v1/sub-user/create": {
    "success": true,
    "message": "Sub-user created",
    "data": {
      "id": "sub-xxxxxxxx",
      "username": "user-xxxxxxxx",
      "password": "***",
      "container_names": ["example-vm"],
      "access_code": "********",
      "created_at": "2026-06-08 16:00:00"
    }
  },
  "GET /api/v1/sub-users": {
    "success": true,
    "data": []
  },
  "POST /api/v1/sub-users/{id}/rotate-password": {
    "success": true,
    "data": { "username": "user-xxxxxxxx", "password": "***", "access_code": "********" }
  },
  "GET /api/v1/sub-users/{id}/audit-logs": {
    "success": true,
    "data": []
  },
  "GET /api/v1/sub-users/{id}/login-logs": {
    "success": true,
    "data": []
  },
  "GET /api/v1/audit-logs": {
    "success": true,
    "data": [
      { "time": "2026-06-08 15:44:40", "action": "apikey.create", "target": "Test", "detail": "scopes=*", "user": "admin", "success": true }
    ]
  },
  "GET /api/v1/login-logs": {
    "success": true,
    "data": [
      { "time": "2026-06-08 08:24:00 UTC", "username": "admin", "ip": "198.51.100.23", "user_agent": "Mozilla/5.0 ...", "success": true }
    ]
  },
  "GET /api/v1/api-keys": {
    "success": true,
    "data": [
      { "id": "c271023f", "name": "Test", "prefix": "clicd_sk_dd9d...", "ip_whitelist": "", "created_at": "2026-06-08 15:44:40", "last_used": "2026-06-08 15:46:10", "scopes": ["*"], "expires_at": "", "disabled": false, "container_uuids": [], "last_used_ip": "198.51.100.23" }
    ]
  },
  "POST /api/v1/api-keys": {
    "success": true,
    "message": "API key created. Save this key now - it won't be shown again.",
    "data": { "id": "a1b2c3d4", "name": "Automation", "key": "clicd_sk_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx", "prefix": "clicd_sk_xxxx...", "ip_whitelist": "198.51.100.23", "scopes": ["dashboard:read", "container:read"], "expires_at": "2026-12-31 23:59:59", "disabled": false, "container_uuids": ["00000000-0000-4000-8000-000000000005"] }
  },
  "PATCH /api/v1/api-keys/{id}": {
    "success": true,
    "data": { "id": "a1b2c3d4", "name": "Automation", "prefix": "clicd_sk_xxxx...", "scopes": ["dashboard:read", "container:read"], "expires_at": "2026-12-31 23:59:59", "disabled": false, "container_uuids": ["00000000-0000-4000-8000-000000000005"] }
  },
  "DELETE /api/v1/api-keys/{id}": {
    "success": true,
    "message": "API key deleted"
  }
}
```
