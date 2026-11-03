package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"clicd/internal/config"
)

// Reuse the test executable as a portable qemu-img stub. The real byte-level
// preflight still runs; only the external info/check commands are simulated.
func init() {
	name := strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe")
	if os.Getenv("CLICD_UPLOAD_QEMU_HELPER") != "1" || name != "qemu-img" && name != "df" && name != "findmnt" {
		return
	}
	if name == "df" {
		fmt.Print("Filesystem 1B-blocks Used Available Use% MountedOn\nfixture 137438953472 0 137438953472 0% /\n")
		os.Exit(0)
	}
	if name == "findmnt" {
		fmt.Println(os.Getenv("CLICD_UPLOAD_MOUNT"))
		os.Exit(0)
	}
	if log := os.Getenv("CLICD_UPLOAD_QEMU_LOG"); log != "" {
		file, _ := os.OpenFile(log, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
		if file != nil {
			fmt.Fprintln(file, strings.Join(os.Args[1:], " "))
			file.Close()
		}
	}
	if os.Getenv("CLICD_UPLOAD_QEMU_BLOCK") == "1" {
		time.Sleep(time.Minute)
	}
	if len(os.Args) > 1 && os.Args[1] == "info" {
		if value := os.Getenv("CLICD_UPLOAD_QEMU_INFO"); value != "" {
			fmt.Print(value)
		} else {
			fmt.Print(`{"format":"qcow2","virtual-size":4294967296}`)
		}
	}
	if len(os.Args) > 1 && os.Args[1] == "check" && os.Getenv("CLICD_UPLOAD_QEMU_CHECK_FAIL") == "1" {
		os.Exit(1)
	}
	os.Exit(0)
}

type uploadPart struct {
	name, filename, value string
	data                  []byte
}

func uploadRequest(t *testing.T, parts []uploadPart) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for _, part := range parts {
		if part.filename != "" {
			file, err := writer.CreateFormFile(part.name, part.filename)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := file.Write(part.data); err != nil {
				t.Fatal(err)
			}
		} else if err := writer.WriteField(part.name, part.value); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/images/upload", bytes.NewReader(body.Bytes()))
	request.Header.Set("Content-Type", writer.FormDataContentType())
	return withAuthContext(request, AuthContext{Type: authTypeAdmin, Username: "upload-test"})
}

func uploadISOFixture() []byte {
	data := make([]byte, 17*2048)
	copy(data[16*2048+1:], "CD001")
	data[16*2048+6] = 1
	return data
}

func uploadQCOWFixture(version uint32) []byte {
	data := make([]byte, 512)
	copy(data, "QFI\xfb")
	binary.BigEndian.PutUint32(data[4:8], version)
	binary.BigEndian.PutUint32(data[20:24], 9)
	binary.BigEndian.PutUint64(data[24:32], 4<<30)
	if version == 3 {
		binary.BigEndian.PutUint32(data[96:100], 4)
		binary.BigEndian.PutUint32(data[100:104], 104)
	}
	return data
}

func TestQCOWUploadUnsafeHeadersNeverInvokeQEMU(t *testing.T) {
	t.Setenv("PATH", "")
	cases := []struct {
		name string
		edit func([]byte)
	}{
		{"backing-offset", func(b []byte) { binary.BigEndian.PutUint64(b[8:16], 104) }},
		{"backing-size", func(b []byte) { binary.BigEndian.PutUint32(b[16:20], 12) }},
		{"encryption", func(b []byte) { binary.BigEndian.PutUint32(b[32:36], 2) }},
		{"snapshot-count", func(b []byte) { binary.BigEndian.PutUint32(b[60:64], 1) }},
		{"snapshot-offset-with-zero-count", func(b []byte) { binary.BigEndian.PutUint64(b[64:72], 512) }},
		{"external-data-bit", func(b []byte) { binary.BigEndian.PutUint64(b[72:80], 4) }},
		{"raw-data-bit", func(b []byte) { binary.BigEndian.PutUint64(b[88:96], 2) }},
		{"unknown-feature", func(b []byte) { binary.BigEndian.PutUint64(b[72:80], 1<<63) }},
		{"oversized-cluster", func(b []byte) { binary.BigEndian.PutUint32(b[20:24], 31) }},
		{"unaligned-header", func(b []byte) { binary.BigEndian.PutUint32(b[100:104], 105) }},
		{"header-past-cluster", func(b []byte) { binary.BigEndian.PutUint32(b[100:104], 1024) }},
		{"external-data-extension", func(b []byte) { binary.BigEndian.PutUint32(b[104:108], 0x44415441) }},
		{"backing-format-extension", func(b []byte) { binary.BigEndian.PutUint32(b[104:108], 0xe2792aca) }},
		{"crypto-pointer-extension", func(b []byte) { binary.BigEndian.PutUint32(b[104:108], 0x0537be77) }},
		{"oversized-extension", func(b []byte) {
			binary.BigEndian.PutUint32(b[104:108], 0x6803f857)
			binary.BigEndian.PutUint32(b[108:112], ^uint32(0))
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := uploadQCOWFixture(3)
			tc.edit(data)
			file := filepath.Join(t.TempDir(), "unsafe.qcow2")
			if err := os.WriteFile(file, data, 0600); err != nil {
				t.Fatal(err)
			}
			err := validateUploadedQCOW2(context.Background(), file)
			if err == nil || strings.Contains(err.Error(), "qemu-img") {
				t.Fatalf("unsafe header reached external qemu-img validation: %v", err)
			}
		})
	}
}

func TestImageUploadStreamingLimitAndCancellation(t *testing.T) {
	var destination bytes.Buffer
	if n, err := copyImageUpload(context.Background(), &destination, strings.NewReader(strings.Repeat("x", 18)), 16); err != errLocalImageTooLarge || n != 17 || destination.Len() != 17 {
		t.Fatalf("streaming byte limit not enforced: n=%d err=%v written=%d", n, err, destination.Len())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	destination.Reset()
	if n, err := copyImageUpload(ctx, &destination, strings.NewReader("image"), 16); err != context.Canceled || n != 0 || destination.Len() != 0 {
		t.Fatalf("canceled stream was written: n=%d err=%v written=%d", n, err, destination.Len())
	}
}

func TestImageUploadMultipartPublicationAndRollback(t *testing.T) {
	const fixtureEnv = "CLICD_UPLOAD_FIXTURE_DIR"
	dir := os.Getenv(fixtureEnv)
	if dir == "" {
		executable, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(executable, "-test.run=^TestImageUploadMultipartPublicationAndRollback$", "-test.v")
		cmd.Env = append(os.Environ(), fixtureEnv+"="+t.TempDir())
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("isolated upload fixture: %v\n%s", err, output)
		} else {
			t.Log(string(output))
		}
		return
	}
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	pool := filepath.Join(dir, "pool")
	if err := os.MkdirAll(pool, 0755); err != nil {
		t.Fatal(err)
	}
	cfg := config.ClicdConfig{AdminUser: "test", AdminPassHash: "hash", JWTSecret: "secret", Port: 8999, DataDir: dir,
		StoragePools: []config.StoragePool{{ID: "upload", Name: "upload", Path: pool, MountPoint: filepath.VolumeName(pool) + string(os.PathSeparator), Enabled: true, ContentTypes: []string{config.StorageContentImages}, DefaultContents: []string{config.StorageContentImages}}}}
	data, _ := json.Marshal(cfg)
	if err := os.WriteFile(filepath.Join(dir, "config.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	config.SetConfigPath(filepath.Join(dir, "config.json"))
	if _, err := config.InitConfig(); err != nil {
		t.Fatal(err)
	}
	// Production normalization maps mount points to their managed /clicd path.
	// Keep this isolated fixture inside its temporary pool on every platform.
	config.AppConfig.StoragePools = append([]config.StoragePool(nil), cfg.StoragePools...)
	config.AppConfig.StoragePools[0].MountPoint = ""
	tools := filepath.Join(dir, "tools")
	if err := os.MkdirAll(tools, 0755); err != nil {
		t.Fatal(err)
	}
	executable, _ := os.Executable()
	source, err := os.Open(executable)
	if err != nil {
		t.Fatal(err)
	}
	tool := filepath.Join(tools, "qemu-img")
	if runtime.GOOS == "windows" {
		tool += ".exe"
	}
	dst, err := os.OpenFile(tool, os.O_CREATE|os.O_WRONLY, 0755)
	if err != nil {
		t.Fatal(err)
	}
	_, copyErr := io.Copy(dst, source)
	source.Close()
	dst.Close()
	if copyErr != nil {
		t.Fatal(copyErr)
	}
	for _, name := range []string{"df", "findmnt"} {
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		if err := os.Link(tool, filepath.Join(tools, name)); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", tools)
	t.Setenv("CLICD_UPLOAD_QEMU_HELPER", "1")
	t.Setenv("CLICD_UPLOAD_MOUNT", config.AppConfig.StoragePools[0].MountPoint)
	if output, err := exec.Command("findmnt").CombinedOutput(); err != nil {
		t.Fatalf("fixture findmnt: %v %s", err, output)
	}
	if output, err := exec.Command("df").CombinedOutput(); err != nil {
		t.Fatalf("fixture df: %v %s", err, output)
	}
	log := filepath.Join(dir, "qemu.log")
	t.Setenv("CLICD_UPLOAD_QEMU_LOG", log)
	cache := filepath.Join(pool, "images", "kvm")
	reset := func() {
		t.Helper()
		config.AppConfig.CustomKVMImages = nil
		config.AppConfig.EnabledImages = nil
		if err := config.SaveConfig(); err != nil {
			t.Fatal(err)
		}
		entries, _ := os.ReadDir(cache)
		for _, entry := range entries {
			if err := os.Remove(filepath.Join(cache, entry.Name())); err != nil {
				t.Fatal(err)
			}
		}
		os.Remove(log)
	}
	assertEmpty := func() {
		t.Helper()
		entries, _ := os.ReadDir(cache)
		if len(entries) != 0 || len(config.AppConfig.CustomKVMImages) != 0 || len(config.AppConfig.EnabledImages) != 0 {
			t.Fatalf("failed upload left stage/cache/metadata: files=%v images=%+v enabled=%v", entries, config.AppConfig.CustomKVMImages, config.AppConfig.EnabledImages)
		}
	}
	valid := []uploadPart{{name: "name", value: "Uploaded image"}, {name: "file", filename: "image.iso", data: uploadISOFixture()}}
	for _, tc := range []struct {
		name  string
		parts []uploadPart
	}{
		{"no-file", valid[:1]},
		{"empty-file", []uploadPart{{name: "name", value: "Empty"}, {name: "file", filename: "empty.iso"}}},
		{"duplicate-file", append(append([]uploadPart{}, valid...), valid[1])},
		{"wrong-file-field", []uploadPart{{name: "name", value: "Wrong"}, {name: "image", filename: "image.iso", data: uploadISOFixture()}}},
		{"duplicate-field", append(append([]uploadPart{}, valid...), uploadPart{name: "name", value: "duplicate"})},
		{"unknown-field", append(append([]uploadPart{}, valid...), uploadPart{name: "path", value: "../../outside.iso"})},
		{"oversized-field", []uploadPart{{name: "name", value: strings.Repeat("n", 8193)}, {name: "file", filename: "image.iso", data: uploadISOFixture()}}},
		{"invalid-format", []uploadPart{{name: "name", value: "HTML"}, {name: "file", filename: "image.iso", data: []byte("<html>not an image</html>")}}},
		{"sha-mismatch", append(append([]uploadPart{}, valid...), uploadPart{name: "sha256", value: strings.Repeat("0", 64)})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reset()
			response := httptest.NewRecorder()
			HandleImageUpload(response, uploadRequest(t, tc.parts))
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status=%d: %s", response.Code, response.Body.String())
			}
			if strings.Contains(response.Body.String(), "storage") {
				t.Fatalf("test did not reach upload validation: %s", response.Body.String())
			}
			assertEmpty()
		})
	}
	reset()
	t.Run("explicit-pool-never-falls-back", func(t *testing.T) {
		request := uploadRequest(t, valid)
		request.URL.RawQuery = "storage_pool_id=missing-pool"
		response := httptest.NewRecorder()
		HandleImageUpload(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("upload silently used a different pool: %d %s", response.Code, response.Body.String())
		}
		assertEmpty()
	})
	t.Run("content-length-limit", func(t *testing.T) {
		request := uploadRequest(t, valid)
		request.ContentLength = maxLocalImageBytes + (1 << 20) + 1
		response := httptest.NewRecorder()
		HandleImageUpload(response, request)
		if response.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("status=%d", response.Code)
		}
		assertEmpty()
	})
	t.Run("canceled-request", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		response := httptest.NewRecorder()
		HandleImageUpload(response, uploadRequest(t, valid).WithContext(ctx))
		if response.Code != 400 {
			t.Fatalf("status=%d", response.Code)
		}
		assertEmpty()
	})
	for name, info := range map[string]string{
		"invalid-qemu-json":          `{`,
		"qemu-reported-backing":      `{"format":"qcow2","virtual-size":4294967296,"backing-filename":"/host/private"}`,
		"qemu-reported-full-backing": `{"format":"qcow2","virtual-size":4294967296,"full-backing-filename":"/host/private"}`,
		"qemu-reported-nested-data":  `{"format":"qcow2","virtual-size":4294967296,"format-specific":{"data":{"data-file":"/host/private"}}}`,
		"qemu-reported-encryption":   `{"format":"qcow2","virtual-size":4294967296,"encrypted":true}`,
		"qemu-reported-snapshot":     `{"format":"qcow2","virtual-size":4294967296,"snapshots":[{"id":"1"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			reset()
			t.Setenv("CLICD_UPLOAD_QEMU_INFO", info)
			response := httptest.NewRecorder()
			HandleImageUpload(response, uploadRequest(t, []uploadPart{{name: "name", value: "Disk"}, {name: "file", filename: "disk.qcow2", data: uploadQCOWFixture(3)}}))
			if response.Code != 400 {
				t.Fatalf("status=%d: %s", response.Code, response.Body.String())
			}
			assertEmpty()
			commands, _ := os.ReadFile(log)
			if !strings.Contains(string(commands), "info ") || strings.Contains(string(commands), "check ") {
				t.Fatalf("unsafe info result reached check: %s", commands)
			}
		})
	}
	t.Run("qemu-consistency-failure", func(t *testing.T) {
		reset()
		t.Setenv("CLICD_UPLOAD_QEMU_CHECK_FAIL", "1")
		response := httptest.NewRecorder()
		HandleImageUpload(response, uploadRequest(t, []uploadPart{{name: "name", value: "Disk"}, {name: "file", filename: "disk.qcow2", data: uploadQCOWFixture(3)}}))
		if response.Code != 400 {
			t.Fatalf("status=%d: %s", response.Code, response.Body.String())
		}
		assertEmpty()
		commands, _ := os.ReadFile(log)
		if !strings.Contains(string(commands), "check ") {
			t.Fatal("consistency check was not exercised")
		}
	})
	t.Run("cancellation-during-qemu", func(t *testing.T) {
		reset()
		t.Setenv("CLICD_UPLOAD_QEMU_BLOCK", "1")
		request := uploadRequest(t, []uploadPart{{name: "name", value: "Disk"}, {name: "file", filename: "disk.qcow2", data: uploadQCOWFixture(3)}})
		ctx, cancel := context.WithCancel(request.Context())
		defer cancel()
		response := httptest.NewRecorder()
		done := make(chan struct{})
		go func() { HandleImageUpload(response, request.WithContext(ctx)); close(done) }()
		deadline := time.Now().Add(3 * time.Second)
		for {
			commands, _ := os.ReadFile(log)
			if strings.Contains(string(commands), "info ") {
				break
			}
			if time.Now().After(deadline) {
				cancel()
				<-done
				t.Fatal("qemu validation never started")
			}
			time.Sleep(10 * time.Millisecond)
		}
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Fatal("upload validator ignored context cancellation")
		}
		if response.Code != 400 {
			t.Fatalf("status=%d", response.Code)
		}
		assertEmpty()
	})
	for _, tc := range []struct {
		name, provisioner, format string
		bytes                     []byte
	}{
		{"iso-path-traversal-filename", "", "iso", uploadISOFixture()},
		{"manual-disk-v2", "manual-disk", "qcow2", uploadQCOWFixture(2)},
		{"manual-disk-v3", "", "qcow2", uploadQCOWFixture(3)},
		{"cloud-init", "linux-cloud-init", "qcow2", uploadQCOWFixture(3)},
		{"windows10", "windows-10", "iso", uploadISOFixture()},
		{"windows11", "windows-11", "iso", uploadISOFixture()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reset()
			sum := sha256.Sum256(tc.bytes)
			sha := hex.EncodeToString(sum[:])
			parts := []uploadPart{{name: "name", value: "Local image"}, {name: "sha256", value: strings.ToUpper(sha)}, {name: "provisioner", value: tc.provisioner}, {name: "file", filename: "../../outside.iso", data: tc.bytes}}
			response := httptest.NewRecorder()
			HandleImageUpload(response, uploadRequest(t, parts))
			if strings.HasPrefix(tc.provisioner, "windows-") && runtime.GOARCH != "amd64" {
				if response.Code != 400 {
					t.Fatalf("automatic Windows installation accepted non-amd64 host: %d", response.Code)
				}
				assertEmpty()
				return
			}
			if response.Code != 201 {
				t.Fatalf("status=%d: %s", response.Code, response.Body.String())
			}
			var result struct {
				Data struct {
					ID, Format string
					SHA        string `json:"sha256"`
					Size       int64  `json:"size_bytes"`
				} `json:"data"`
			}
			if json.Unmarshal(response.Body.Bytes(), &result) != nil {
				t.Fatal("invalid response")
			}
			image := config.AppConfig.CustomKVMImages[0]
			if result.Data.ID != image.ID || result.Data.Format != tc.format || result.Data.SHA != sha || result.Data.Size != int64(len(tc.bytes)) {
				t.Fatalf("incorrect upload receipt: %s", response.Body.String())
			}
			entries, err := os.ReadDir(cache)
			if err != nil || len(entries) != 1 || entries[0].Name() != image.ID+"."+tc.format || !strings.HasPrefix(image.ID, "custom-kvm-") || image.SHA256 != sha || len(config.AppConfig.EnabledImages) != 1 || config.AppConfig.EnabledImages[0] != image.ID {
				t.Fatalf("upload was not atomically registered: %s files=%v image=%+v", response.Body.String(), entries, image)
			}
			if _, err := os.Stat(filepath.Join(pool, "outside.iso")); !os.IsNotExist(err) {
				t.Fatal("client filename escaped cache")
			}
			if image.URL != "" {
				t.Fatal("local upload acquired an external source URL")
			}
		})
	}
	reset()
	t.Run("publication-database-rollback", func(t *testing.T) {
		db, err := sql.Open("sqlite", filepath.Join(dir, "config.db"))
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		if _, err = db.Exec(`CREATE TRIGGER reject_upload BEFORE INSERT ON enabled_images BEGIN SELECT RAISE(ABORT,'reject upload'); END`); err != nil {
			t.Fatal(err)
		}
		defer db.Exec(`DROP TRIGGER reject_upload`)
		response := httptest.NewRecorder()
		HandleImageUpload(response, uploadRequest(t, valid))
		if response.Code != 400 {
			t.Fatalf("status=%d: %s", response.Code, response.Body.String())
		}
		assertEmpty()
		var count int
		if err = db.QueryRow(`SELECT COUNT(*) FROM enabled_images`).Scan(&count); err != nil || count != 0 {
			t.Fatalf("partial enabled registration persisted: %d %v", count, err)
		}
	})
}
