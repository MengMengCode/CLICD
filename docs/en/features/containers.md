# Container Management

Container Management is the core CLICD module. It covers creation, lifecycle operations, resource limits, network mappings, traffic statistics, password resets, and console access.

To deploy an application from an OCI image, use [Podman Containers](./podman).

## Container List

The list page scans container status. Administrators can view all containers. Sub-users only see containers within their authorization scope.

Common fields include:

- ID, UUID, and name.
- Virtualization type.
- Runtime status.
- IP and IPv6.
- CPU, memory, and disk limits.
- Traffic usage and traffic limits.
- Expiration time.

## Create Containers

Creation requires a template and resource quotas. Batch creation is available from the panel or API and is useful for issuing multiple containers at once.

```http
POST /api/v1/containers
POST /api/v1/batch-create
```

Linux containers and Linux KVM virtual machines support SSH login configuration during creation:

- `auto_password`: generate a root SSH password automatically.
- `password`: use a custom `ssh_password`.
- `key`: write a one-line `ssh_public_key`; a password is still kept for WebSSH.

Network allocation can combine NAT, public IPv4, and IPv6 as needed. API fields such as `assign_nat`, `assign_ipv4`, `public_ipv4s`, `assign_ipv6`, and `ipv6_addresses` are optional. If they are omitted, default behavior is preserved.

LXC creation also supports two isolation options: `nesting=true` enables the nesting capabilities needed by Docker/LXC-in-LXC and systemd; `privileged=true` creates a privileged container and is restricted to administrators for trusted workloads. Privileged mode reduces isolation, cannot be switched on an existing instance, and should remain `false` by default.

Instance names can be changed independently. The operation updates only the panel display name; the runtime name, UUID, disk paths, MAC address, and guest hostname remain unchanged:

```http
PATCH /api/v1/containers/{id}/name
```

Send `{ "name": "new-name" }`; the endpoint requires the `container:resize` scope.

## Lifecycle Operations

```http
POST /api/v1/containers/{id}/start
POST /api/v1/containers/{id}/stop
POST /api/v1/containers/{id}/restart
POST /api/v1/containers/{id}/reinstall
DELETE /api/v1/containers/{id}/delete
```

Start, stop, reinstall, and delete actions enter the task queue. Call `GET /api/v1/tasks` afterwards to check execution status.

When reinstalling a Linux system, you may pass `ssh_auth_mode`, `ssh_password`, and `ssh_public_key`. `ssh_auth_mode=keep` keeps the current SSH password. If these fields are omitted, the old behavior is preserved.

## Resources and Traffic

The container details page supports resource usage, traffic limit changes, resource limit changes, and expiration changes.

Use disk management in instance details for system-disk growth and data disks while the instance is stopped. See [Disks, Backups and Clones](./storage-backups) for full backup, restore, full clone and instance templates, and [Tasks](./tasks) for progress and errors.

```http
GET /api/v1/containers/{id}/usage
GET /api/v1/containers/{id}/traffic
POST /api/v1/containers/{id}/traffic-reset
PUT /api/v1/containers/{id}/traffic-limit
PUT /api/v1/containers/{id}/resource-limit
PUT /api/v1/containers/{id}/expiry
```

## NAT Port Management

The NAT port management section supports adding, editing, and deleting mappings. Add and edit actions use a dialog so name, protocol, external port, and internal port can be filled in together.

```http
GET /api/v1/containers/{id}/random-port
POST /api/v1/containers/{id}/port-mappings
PUT /api/v1/containers/{id}/port-mappings/{index}
DELETE /api/v1/containers/{id}/port-mappings/{index}
```

In sub-user mode, administrators can limit sub-users to changing only the internal port, preventing changes to the host-facing port and protocol.

## Remote Console

```http
POST /api/v1/ssh-ticket
POST /api/v1/vnc-ticket
```

Tickets are short-lived. Use them immediately for WebSSH or WebVNC and do not persist them.
