package podman

import (
	"fmt"
	"math"
	"path"
	"regexp"
	"strconv"
	"strings"
)

var (
	fullID         = regexp.MustCompile(`^[a-f0-9]{64}$`)
	containerName  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)
	registryHost   = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)*$`)
	repositoryPart = regexp.MustCompile(`^[a-z0-9]+(?:(?:[._]|__|-+)[a-z0-9]+)*$`)
	imageTag       = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$`)
	imageDigest    = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
	environmentKey = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)
	volumeTarget   = regexp.MustCompile(`^/[A-Za-z0-9._-]+(?:/[A-Za-z0-9._-]+)*$`)
)

func (r *CreateRequest) normalize() error {
	r.Name = strings.TrimSpace(r.Name)
	r.Image = strings.TrimSpace(r.Image)
	if !containerName.MatchString(r.Name) {
		return fmt.Errorf("%w: name must start with a letter or number and contain at most 64 letters, numbers, periods, underscores or hyphens", ErrInvalid)
	}
	if !validImage(r.Image) {
		return fmt.Errorf("%w: use a fully qualified image such as docker.io/library/nginx:alpine", ErrInvalid)
	}
	if math.IsNaN(r.CPUs) || r.CPUs < 0 || (r.CPUs != 0 &&
		(r.CPUs < 0.01 || r.CPUs > 256 || math.Abs(r.CPUs*100-math.Round(r.CPUs*100)) > 1e-6)) {
		return fmt.Errorf("%w: CPU limit must be 0 or between 0.01 and 256 with at most two decimal places", ErrInvalid)
	}
	if r.MemoryMB != 0 && (r.MemoryMB < 16 || r.MemoryMB > 1048576) {
		return fmt.Errorf("%w: memory limit must be 0 or between 16 and 1048576 MiB", ErrInvalid)
	}
	if len(r.Ports) > 64 || len(r.Volumes) > 16 || len(r.Env) > 64 || len(r.Command) > 64 {
		return fmt.Errorf("%w: too many ports, volumes, environment variables or command arguments", ErrInvalid)
	}
	if err := r.normalizePorts(); err != nil {
		return err
	}
	seenVolumes := make(map[string]bool, len(r.Volumes))
	for _, target := range r.Volumes {
		if !validVolumeTarget(target) || seenVolumes[target] {
			return fmt.Errorf("%w: volume targets must be unique, absolute guest data paths", ErrInvalid)
		}
		seenVolumes[target] = true
	}
	for key, value := range r.Env {
		if !environmentKey.MatchString(key) || len(value) > 4096 || strings.ContainsAny(value, "\r\n\x00") {
			return fmt.Errorf("%w: environment variables require valid names and single-line values up to 4096 bytes", ErrInvalid)
		}
	}
	commandBytes := 0
	for _, arg := range r.Command {
		commandBytes += len(arg)
		if commandBytes > 32768 || strings.ContainsRune(arg, '\x00') {
			return fmt.Errorf("%w: the command must be at most 32768 bytes and cannot contain NUL", ErrInvalid)
		}
	}
	return nil
}

func (r *CreateRequest) normalizePorts() error {
	seenPorts := make(map[string]bool, len(r.Ports))
	for i := range r.Ports {
		p := &r.Ports[i]
		p.Protocol = strings.ToLower(strings.TrimSpace(p.Protocol))
		if p.Protocol == "" {
			p.Protocol = "tcp"
		}
		if !validPort(p.HostPort) || !validPort(p.ContainerPort) || (p.Protocol != "tcp" && p.Protocol != "udp") {
			return fmt.Errorf("%w: ports must be between 1 and 65535 and use TCP or UDP", ErrInvalid)
		}
		binding := strconv.Itoa(p.HostPort) + "/" + p.Protocol
		if seenPorts[binding] {
			return fmt.Errorf("%w: duplicate host port %s", ErrInvalid, binding)
		}
		seenPorts[binding] = true
	}
	return nil
}

func validPort(port int) bool {
	return port >= 1 && port <= 65535
}

func validImage(reference string) bool {
	if len(reference) > 512 {
		return false
	}
	name, digest, pinned := strings.Cut(reference, "@")
	if pinned && !imageDigest.MatchString(digest) {
		return false
	}
	registry, repository, qualified := strings.Cut(name, "/")
	if !qualified {
		return false
	}
	host, port, withPort := strings.Cut(registry, ":")
	// Requiring a DNS name or localhost also prevents a colon from being
	// interpreted as an image transport (for example, dir: or docker-archive:).
	if !registryHost.MatchString(host) || (host != "localhost" && !strings.Contains(host, ".")) {
		return false
	}
	if withPort {
		n, err := strconv.Atoi(port)
		if err != nil || !validPort(n) || strconv.Itoa(n) != port {
			return false
		}
	}
	repo, tag, tagged := strings.Cut(repository, ":")
	if tagged && !imageTag.MatchString(tag) {
		return false
	}
	for _, component := range strings.Split(repo, "/") {
		if !repositoryPart.MatchString(component) {
			return false
		}
	}
	return true
}

func validVolumeTarget(target string) bool {
	if len(target) > 256 || !volumeTarget.MatchString(target) || path.Clean(target) != target {
		return false
	}
	for _, restricted := range []string{"/proc", "/sys", "/dev", "/etc/hosts", "/etc/hostname", "/etc/resolv.conf", "/run/.containerenv"} {
		if target == restricted || strings.HasPrefix(target, restricted+"/") || strings.HasPrefix(restricted, target+"/") {
			return false
		}
	}
	return true
}
