package podman

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	managedLabel  = "io.clicd.managed"
	managedValue  = "podman"
	quickTimeout  = 20 * time.Second
	createTimeout = 3 * time.Minute
	logLimit      = 128 << 10
)

var (
	ErrInvalid      = errors.New("invalid Podman application configuration")
	ErrNotFound     = errors.New("Podman application not found")
	ErrNotRunning   = errors.New("Podman application is not running")
	ErrUnavailable  = errors.New("Podman is unavailable")
	ErrPortConflict = errors.New("Podman host port is already in use")
)

// Manager controls the local Podman engine and its CLICD-managed applications.
type Manager struct{}

type Info struct {
	Version string `json:"version"`
}

type PortMapping struct {
	HostPort      int    `json:"host_port"`
	ContainerPort int    `json:"container_port"`
	Protocol      string `json:"protocol"`
}

type CreateRequest struct {
	Name     string            `json:"name"`
	Image    string            `json:"image"`
	CPUs     float64           `json:"cpus"`
	MemoryMB int               `json:"memory_mb"`
	Ports    []PortMapping     `json:"ports"`
	Env      map[string]string `json:"env"`
	Command  []string          `json:"command"`
	Volumes  []string          `json:"volumes"`
}

type Container struct {
	ID         string        `json:"id"`
	Name       string        `json:"name"`
	Image      string        `json:"image"`
	Status     string        `json:"status"`
	CreatedAt  time.Time     `json:"created_at"`
	Ports      []PortMapping `json:"ports,omitempty"`
	MemoryMB   int64         `json:"memory_mb,omitempty"`
	CPUs       float64       `json:"cpus,omitempty"`
	Volumes    []string      `json:"volumes,omitempty"`
	ExitCode   *int          `json:"exit_code,omitempty"`
	StartedAt  *time.Time    `json:"started_at,omitempty"`
	FinishedAt *time.Time    `json:"finished_at,omitempty"`
	Health     string        `json:"health,omitempty"`
}

type inspectHealth struct {
	Status string `json:"Status"`
}

type inspectContainer struct {
	ID        string    `json:"Id"`
	Name      string    `json:"Name"`
	Image     string    `json:"Image"`
	ImageName string    `json:"ImageName"`
	Created   time.Time `json:"Created"`
	State     struct {
		Status      string        `json:"Status"`
		ExitCode    *int          `json:"ExitCode"`
		StartedAt   time.Time     `json:"StartedAt"`
		FinishedAt  time.Time     `json:"FinishedAt"`
		Healthcheck inspectHealth `json:"Healthcheck"`
		Health      inspectHealth `json:"Health"`
	} `json:"State"`
	Config struct {
		Labels map[string]string `json:"Labels"`
	} `json:"Config"`
	HostConfig struct {
		Memory       int64 `json:"Memory"`
		CPUQuota     int64 `json:"CpuQuota"`
		CPUPeriod    int64 `json:"CpuPeriod"`
		NanoCPUs     int64 `json:"NanoCpus"`
		PortBindings map[string][]struct {
			HostPort string `json:"HostPort"`
		} `json:"PortBindings"`
	} `json:"HostConfig"`
	Mounts []struct {
		Type        string `json:"Type"`
		Destination string `json:"Destination"`
	} `json:"Mounts"`
}

func (c inspectContainer) managed() bool {
	return fullID.MatchString(c.ID) && c.Config.Labels[managedLabel] == managedValue
}

func (c inspectContainer) app() Container {
	out := Container{ID: c.ID, Name: strings.TrimPrefix(c.Name, "/"), Image: c.ImageName, Status: c.State.Status, CreatedAt: c.Created}
	if out.Image == "" {
		out.Image = c.Image
	}
	switch out.Status {
	case "configured", "initialized":
		out.Status = "created"
	case "exited":
		out.Status = "stopped"
	case "":
		out.Status = "unknown"
	}
	if !c.State.StartedAt.IsZero() {
		out.StartedAt = &c.State.StartedAt
	}
	if out.Status == "stopped" && !c.State.FinishedAt.IsZero() {
		out.FinishedAt = &c.State.FinishedAt
		out.ExitCode = c.State.ExitCode
	}
	if out.Status == "running" || out.Status == "paused" {
		out.Health = c.State.Health.Status
		if out.Health == "" {
			out.Health = c.State.Healthcheck.Status
		}
	}
	if c.HostConfig.Memory > 0 {
		out.MemoryMB = c.HostConfig.Memory / (1 << 20)
	}
	if c.HostConfig.NanoCPUs > 0 {
		out.CPUs = float64(c.HostConfig.NanoCPUs) / 1e9
	} else if c.HostConfig.CPUPeriod > 0 && c.HostConfig.CPUQuota > 0 {
		out.CPUs = float64(c.HostConfig.CPUQuota) / float64(c.HostConfig.CPUPeriod)
	}
	for port, bindings := range c.HostConfig.PortBindings {
		value, protocol, ok := strings.Cut(port, "/")
		containerPort, err := strconv.Atoi(value)
		if !ok || err != nil || !validPort(containerPort) || (protocol != "tcp" && protocol != "udp") {
			continue
		}
		for _, binding := range bindings {
			if hostPort, err := strconv.Atoi(binding.HostPort); err == nil && validPort(hostPort) {
				out.Ports = append(out.Ports, PortMapping{hostPort, containerPort, protocol})
			}
		}
	}
	sort.Slice(out.Ports, func(i, j int) bool {
		a, b := out.Ports[i], out.Ports[j]
		if a.HostPort != b.HostPort {
			return a.HostPort < b.HostPort
		}
		if a.Protocol != b.Protocol {
			return a.Protocol < b.Protocol
		}
		return a.ContainerPort < b.ContainerPort
	})
	for _, mount := range c.Mounts {
		if mount.Type == "volume" {
			out.Volumes = append(out.Volumes, mount.Destination)
		}
	}
	sort.Strings(out.Volumes)
	return out
}

func (Manager) Info(ctx context.Context) (Info, error) {
	out, err := run(ctx, quickTimeout, "info", "--format", "{{.Version.Version}}")
	if err != nil {
		if errors.Is(err, ErrUnavailable) {
			return Info{}, err
		}
		return Info{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	version := strings.TrimSpace(string(out))
	if version == "" {
		return Info{}, fmt.Errorf("%w: the engine did not report a version", ErrUnavailable)
	}
	return Info{Version: version}, nil
}

func (m Manager) List(ctx context.Context) ([]Container, error) {
	ctx, cancel := context.WithTimeout(ctx, quickTimeout)
	defer cancel()
	out, err := run(ctx, quickTimeout, "ps", "-a", "--filter", "label="+managedLabel+"="+managedValue, "--no-trunc", "--format", "{{.ID}}")
	if err != nil {
		return nil, err
	}
	ids := strings.Fields(string(out))
	for _, id := range ids {
		if !fullID.MatchString(id) {
			return nil, errors.New("Podman returned an invalid container ID")
		}
	}
	apps := make([]Container, 0, len(ids))
	for offset := 0; offset < len(ids); offset += 32 {
		group := ids[offset:min(offset+32, len(ids))]
		inspected, err := inspect(ctx, group...)
		if err != nil {
			// A container may have been removed since the list was obtained.
			for _, id := range group {
				app, err := m.inspectOwned(ctx, id)
				if errors.Is(err, ErrNotFound) {
					continue
				}
				if err != nil {
					return nil, err
				}
				apps = append(apps, app.app())
			}
			continue
		}
		for _, container := range inspected {
			if container.managed() {
				apps = append(apps, container.app())
			}
		}
	}
	return apps, nil
}

func (m Manager) Create(ctx context.Context, req CreateRequest) (Container, error) {
	if err := req.normalize(); err != nil {
		return Container{}, err
	}
	dir, err := os.MkdirTemp("", "clicd-podman-")
	if err != nil {
		return Container{}, err
	}
	defer os.RemoveAll(dir)
	cidFile := filepath.Join(dir, "cid")
	args := []string{
		"create", "--cidfile", cidFile, "--name", req.Name,
		"--label", managedLabel + "=" + managedValue, "--pull=missing",
		"--security-opt=no-new-privileges", "--privileged=false",
		"--network=bridge", "--pid=private", "--ipc=private", "--uts=private",
		"--http-proxy=false", "--pids-limit=1024", "--log-driver=k8s-file",
		"--log-opt=max-size=10mb", "--image-volume=ignore",
	}
	if req.CPUs > 0 {
		args = append(args, "--cpus", strconv.FormatFloat(req.CPUs, 'f', 2, 64))
	}
	if req.MemoryMB > 0 {
		args = append(args, "--memory", strconv.Itoa(req.MemoryMB)+"m")
	}
	if len(req.Env) > 0 {
		// Keep secrets out of argv and Podman's recorded creation command.
		envFile := filepath.Join(dir, "environment")
		if err := writeEnvFile(envFile, req.Env); err != nil {
			return Container{}, err
		}
		args = append(args, "--env-file", envFile)
	}
	for _, p := range req.Ports {
		args = append(args, "--publish", fmt.Sprintf("0.0.0.0:%d:%d/%s", p.HostPort, p.ContainerPort, p.Protocol))
	}
	for _, destination := range req.Volumes {
		args = append(args, "--volume", destination)
	}
	args = append(args, "--", req.Image)
	args = append(args, req.Command...)
	out, createErr := run(ctx, createTimeout, args...)
	id := strings.TrimSpace(string(out))
	if file, err := os.ReadFile(cidFile); err == nil {
		id = strings.TrimSpace(string(file))
	}
	if !fullID.MatchString(id) {
		if createErr != nil {
			return Container{}, createErr
		}
		return Container{}, errors.New("Podman did not return the new application's ID")
	}
	if createErr != nil {
		return Container{}, m.cleanupFailedCreate(id, createErr)
	}
	if err := m.Start(ctx, id); err != nil {
		return Container{}, m.cleanupFailedCreate(id, err)
	}
	container, err := m.inspectOwned(ctx, id)
	if err != nil {
		return Container{}, m.cleanupFailedCreate(id, err)
	}
	return container.app(), nil
}

func writeEnvFile(path string, env map[string]string) error {
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var contents strings.Builder
	for _, key := range keys {
		fmt.Fprintf(&contents, "%s=%s\n", key, env[key])
	}
	return os.WriteFile(path, []byte(contents.String()), 0600)
}

func (m Manager) cleanupFailedCreate(id string, cause error) error {
	if err := m.Delete(context.Background(), id); err != nil && !errors.Is(err, ErrNotFound) {
		return fmt.Errorf("%w; cleanup failed: %v", cause, err)
	}
	return cause
}

func (m Manager) Start(ctx context.Context, id string) error {
	return m.action(ctx, id, "start")
}

func (m Manager) Stop(ctx context.Context, id string) error {
	return m.action(ctx, id, "stop", "--time", "10")
}

func (m Manager) Restart(ctx context.Context, id string) error {
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	container, err := m.inspectOwned(ctx, id)
	if err != nil {
		return err
	}
	switch container.State.Status {
	case "running", "paused":
		// Podman 3.4 may try to reserve the old container's published ports
		// before its monitor releases them during a native restart.
		if err := m.Stop(ctx, id); err != nil {
			return err
		}
	default:
		return m.Start(ctx, id)
	}
	for attempt := 0; ; attempt++ {
		err := m.Start(ctx, id)
		if !errors.Is(err, ErrPortConflict) || attempt == 2 {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(attempt+1) * 250 * time.Millisecond):
		}
	}
}

func (m Manager) Delete(ctx context.Context, id string) error {
	return m.action(ctx, id, "rm", "--force", "--ignore", "--volumes")
}

func (m Manager) action(ctx context.Context, id string, args ...string) error {
	if _, err := m.inspectOwned(ctx, id); err != nil {
		return err
	}
	_, err := run(ctx, quickTimeout, append(args, "--", id)...)
	if args[0] == "start" && err != nil && strings.Contains(strings.ToLower(err.Error()), "address already in use") {
		return fmt.Errorf("%w: %v", ErrPortConflict, err)
	}
	return err
}

func (m Manager) Logs(ctx context.Context, id string) (string, error) {
	if _, err := m.inspectOwned(ctx, id); err != nil {
		return "", err
	}
	callCtx, cancel := context.WithTimeout(ctx, quickTimeout)
	defer cancel()
	cmd := command(callCtx, "logs", "--tail", "200", "--", id)
	out := &outputBuffer{limit: logLimit, keepTail: true}
	cmd.Stdout, cmd.Stderr = out, out
	if err := cmd.Run(); err != nil {
		return "", commandError(callCtx, err, out.String())
	}
	if out.truncated {
		return "[Earlier log output omitted]\n" + out.String(), nil
	}
	return out.String(), nil
}

func (Manager) inspectOwned(ctx context.Context, id string) (inspectContainer, error) {
	if !fullID.MatchString(id) {
		return inspectContainer{}, fmt.Errorf("%w: a full container ID is required", ErrInvalid)
	}
	containers, err := inspect(ctx, id)
	if err != nil {
		if ctx.Err() != nil || errors.Is(err, ErrUnavailable) || errors.Is(err, context.DeadlineExceeded) {
			return inspectContainer{}, err
		}
		_, existsErr := run(ctx, quickTimeout, "container", "exists", "--", id)
		var exitErr *exec.ExitError
		if errors.As(existsErr, &exitErr) && exitErr.ExitCode() == 1 {
			return inspectContainer{}, ErrNotFound
		}
		return inspectContainer{}, err
	}
	for _, container := range containers {
		if container.ID == id && container.managed() {
			return container, nil
		}
	}
	return inspectContainer{}, ErrNotFound
}

func inspect(ctx context.Context, ids ...string) ([]inspectContainer, error) {
	out, err := run(ctx, quickTimeout, append([]string{"container", "inspect", "--"}, ids...)...)
	if err != nil {
		return nil, err
	}
	var result []inspectContainer
	if err := json.Unmarshal(out, &result); err != nil {
		return nil, fmt.Errorf("invalid Podman inspection response: %w", err)
	}
	return result, nil
}

func command(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "podman", append([]string{"--remote=false"}, args...)...)
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		switch key {
		case "CONTAINER_HOST", "CONTAINER_CONNECTION", "CONTAINER_SSHKEY":
			// A service-wide remote connection must not redirect panel actions.
		default:
			cmd.Env = append(cmd.Env, value)
		}
	}
	cmd.WaitDelay = 2 * time.Second
	return cmd
}

func run(ctx context.Context, timeout time.Duration, args ...string) ([]byte, error) {
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := command(callCtx, args...)
	out := &outputBuffer{limit: 8 << 20}
	stderr := &outputBuffer{limit: 8 << 10, keepTail: true}
	cmd.Stdout, cmd.Stderr = out, stderr
	if err := cmd.Run(); err != nil {
		return out.Bytes(), commandError(callCtx, err, stderr.String())
	}
	if out.truncated {
		return nil, errors.New("Podman response exceeded the output limit")
	}
	return out.Bytes(), nil
}

func commandError(ctx context.Context, err error, detail string) error {
	if ctx.Err() != nil {
		return fmt.Errorf("Podman command interrupted: %w", ctx.Err())
	}
	if errors.Is(err, exec.ErrNotFound) {
		return fmt.Errorf("%w: install Podman on the host", ErrUnavailable)
	}
	if detail = strings.TrimSpace(detail); detail != "" {
		return fmt.Errorf("Podman command failed: %s (%w)", detail, err)
	}
	return fmt.Errorf("Podman command failed: %w", err)
}

// outputBuffer drains the command's pipes without keeping unbounded output.
type outputBuffer struct {
	buffer    bytes.Buffer
	limit     int
	keepTail  bool
	truncated bool
}

func (b *outputBuffer) Bytes() []byte  { return b.buffer.Bytes() }
func (b *outputBuffer) String() string { return b.buffer.String() }

func (b *outputBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if remaining := b.limit - b.buffer.Len(); n > remaining {
		b.truncated = true
		if !b.keepTail {
			p = p[:remaining]
		} else if n >= b.limit {
			b.buffer.Reset()
			p = p[n-b.limit:]
		} else {
			b.buffer.Next(n - remaining)
		}
	}
	_, _ = b.buffer.Write(p)
	return n, nil
}
