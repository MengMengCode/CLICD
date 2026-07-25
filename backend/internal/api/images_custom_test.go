package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
)

func TestCustomKVMImageCreateRejectsInvalidSource(t *testing.T) {
	payload := map[string]string{
		"name":        "Invalid Source",
		"distro":      "ubuntu",
		"release":     "noble",
		"arch":        runtime.GOARCH,
		"url":         "file:///etc/passwd",
		"provisioner": "linux-cloud-init",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/images/custom", bytes.NewReader(body))
	response := httptest.NewRecorder()

	HandleCustomKVMImages(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusBadRequest, response.Body.String())
	}
}

func TestCustomKVMImageCreateRejectsArchitectureMismatch(t *testing.T) {
	otherArch := "arm64"
	if runtime.GOARCH == otherArch {
		otherArch = "amd64"
	}
	payload := map[string]string{
		"name":        "Wrong Architecture",
		"distro":      "ubuntu",
		"release":     "noble",
		"arch":        otherArch,
		"url":         "https://example.test/image.qcow2",
		"provisioner": "linux-cloud-init",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/images/custom", bytes.NewReader(body))
	response := httptest.NewRecorder()

	HandleCustomKVMImages(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusBadRequest, response.Body.String())
	}
}

func TestCustomLXCImageCreateRejectsInvalidSource(t *testing.T) {
	payload := map[string]string{
		"type":    "lxc",
		"name":    "Invalid LXC Source",
		"distro":  "alpine",
		"release": "3.21",
		"arch":    runtime.GOARCH,
		"url":     "file:///tmp/rootfs.tar.xz",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/images/custom", bytes.NewReader(body))
	response := httptest.NewRecorder()

	HandleCustomKVMImages(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusBadRequest, response.Body.String())
	}
}
