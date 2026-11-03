package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestDiskAPINameLimitCountsUnicodeCharacters(t *testing.T) {
	diskAPIFixture(t)
	for _, tc := range []struct {
		name  string
		valid bool
	}{
		{strings.Repeat("盘", 64), true},
		{strings.Repeat("盘", 80), true},
		{strings.Repeat("盘", 81), false},
		{strings.Repeat("x", 80), true},
		{strings.Repeat("x", 81), false},
	} {
		// Deliberately omit size_gb: a valid display name must reach that
		// independent required-field check without touching any runtime disk.
		body, _ := json.Marshal(map[string]string{"name": tc.name})
		response := requestDisk(t, http.MethodPost, "disks", string(body), AuthContext{Type: authTypeAdmin})
		if response.Code != http.StatusBadRequest {
			t.Fatalf("status=%d: %s", response.Code, response.Body.String())
		}
		if tc.valid && !strings.Contains(response.Body.String(), "size_gb is required") {
			t.Fatalf("valid Unicode name failed byte-length validation: %s", response.Body.String())
		}
		if !tc.valid && !strings.Contains(response.Body.String(), "Disk name must contain") {
			t.Fatalf("overlong display name was not rejected: %s", response.Body.String())
		}
	}
}
