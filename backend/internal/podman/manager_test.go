package podman

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestCreateRequestRejectsUnexpectedInputs(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*CreateRequest)
	}{
		{"path image transport", func(r *CreateRequest) { r.Image = "dir:123/path" }},
		{"archive image transport", func(r *CreateRequest) { r.Image = "docker-archive:123/image" }},
		{"short image name", func(r *CreateRequest) { r.Image = "ubuntu:24.04" }},
		{"image option", func(r *CreateRequest) { r.Image = "--privileged" }},
		{"host bind", func(r *CreateRequest) { r.Volumes = []string{"/etc:/data"} }},
		{"kernel mount", func(r *CreateRequest) { r.Volumes = []string{"/sys/fs"} }},
		{"duplicate binding", func(r *CreateRequest) { r.Ports = append(r.Ports, PortMapping{8080, 81, "TCP"}) }},
		{"multiline environment", func(r *CreateRequest) { r.Env["TEST"] = "first\nsecond=value" }},
		{"invalid CPU precision", func(r *CreateRequest) { r.CPUs = 1.002 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			req := CreateRequest{
				Name: "demo", Image: "docker.io/library/alpine:3.21", CPUs: 0.25, MemoryMB: 64,
				Ports: []PortMapping{{8080, 80, ""}}, Volumes: []string{"/var/lib/app"},
				Env: map[string]string{"TEST": "a=b"}, Command: []string{"sh", "-c", "echo ok"},
			}
			if err := req.normalize(); err != nil || req.Ports[0].Protocol != "tcp" {
				t.Fatalf("valid request rejected: %v", err)
			}
			test.change(&req)
			if err := req.normalize(); !errors.Is(err, ErrInvalid) {
				t.Fatalf("invalid request accepted or wrong error: %v", err)
			}
		})
	}
}

func TestInspectOlderPodmanDoesNotExposePrivateData(t *testing.T) {
	id := strings.Repeat("3", 64)
	input := `{
		"Id":"` + id + `", "Created":"2026-11-14T08:10:29.158833123Z",
		"Name":"example","ImageName":"docker.io/library/alpine:3.21",
		"State":{"Status":"exited"},"Config":{"Labels":{"io.clicd.managed":"podman"},"Env":["SECRET=private"]},
		"HostConfig":{"CpuPeriod":100000,"CpuQuota":25000,"Memory":67108864,
		"PortBindings":{"80/tcp":[{"HostIp":"0.0.0.0","HostPort":"18895"}],"81/udp":[{"HostIp":"0.0.0.0","HostPort":"18894"}]}},
		"Mounts":[{"Type":"volume","Name":"private-name","Source":"/host/path","Destination":"/data"}]
	}`
	var inspected inspectContainer
	if err := json.Unmarshal([]byte(input), &inspected); err != nil {
		t.Fatal(err)
	}
	app := inspected.app()
	if !inspected.managed() || app.ID != id || app.Status != "stopped" || app.CPUs != 0.25 || app.MemoryMB != 64 ||
		len(app.Ports) != 2 || app.Ports[0] != (PortMapping{18894, 81, "udp"}) || len(app.Volumes) != 1 || app.Volumes[0] != "/data" {
		t.Fatalf("unexpected app: %+v", app)
	}
	serialized, err := json.Marshal(app)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(serialized), "private") || strings.Contains(string(serialized), "/host/path") {
		t.Fatalf("private inspect data appeared in API output: %s", serialized)
	}
	inspected.Config.Labels = nil
	if inspected.managed() {
		t.Fatal("unmanaged container was accepted")
	}
}

func TestOutputBufferKeepsOnlyBoundedTail(t *testing.T) {
	b := &outputBuffer{limit: 6, keepTail: true}
	for _, piece := range []string{"12345", "678", "abcdefghij", "k"} {
		n, err := b.Write([]byte(piece))
		if n != len(piece) || err != nil {
			t.Fatalf("output writer did not drain the input: %d, %v", n, err)
		}
	}
	if !b.truncated || b.String() != "fghijk" {
		t.Fatalf("got %q, truncated=%v", b.String(), b.truncated)
	}
}
