package config

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestUploadedImageMetadataAndEnabledPublishAtomically(t *testing.T) {
	setupHistoryStore(t)
	previous := CustomKVMImage{ID: "custom-kvm-existing", Name: "Existing", Arch: "amd64", Provisioner: KVMProvisionerManualISO}
	AppConfig.CustomKVMImages = []CustomKVMImage{previous}
	AppConfig.EnabledImages = []string{previous.ID}
	if err := SaveConfig(); err != nil {
		t.Fatal(err)
	}
	image := CustomKVMImage{ID: "custom-kvm-new", Name: "Uploaded", Arch: "amd64", Provisioner: KVMProvisionerManualISO, SHA256: "sha"}
	if _, err := db.Exec(`CREATE TRIGGER reject_uploaded BEFORE INSERT ON enabled_images WHEN NEW.image_id = 'custom-kvm-new' BEGIN SELECT RAISE(ABORT,'forced failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := AddUploadedKVMImage(image); err == nil {
		t.Fatal("forced metadata failure was accepted")
	}
	if !reflect.DeepEqual(AppConfig.CustomKVMImages, []CustomKVMImage{previous}) || !reflect.DeepEqual(AppConfig.EnabledImages, []string{previous.ID}) {
		t.Fatal("failed registration changed in-memory images or enabled flags")
	}
	var raw string
	if err := db.QueryRow(`SELECT value FROM app_meta WHERE key = 'custom_kvm_images'`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var persisted []CustomKVMImage
	if err := json.Unmarshal([]byte(raw), &persisted); err != nil || !reflect.DeepEqual(persisted, []CustomKVMImage{previous}) {
		t.Fatalf("failed image metadata escaped transaction: %s %v", raw, err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM enabled_images`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("failed registration changed stored flags: %d %v", count, err)
	}
	if _, err := db.Exec(`DROP TRIGGER reject_uploaded`); err != nil {
		t.Fatal(err)
	}
	if err := AddUploadedKVMImage(image); err != nil {
		t.Fatal(err)
	}
	if len(AppConfig.CustomKVMImages) != 2 || len(AppConfig.EnabledImages) != 2 {
		t.Fatal("successful registration did not publish both lists")
	}
	loaded, ok, err := loadConfigFromDB()
	if err != nil || !ok || !reflect.DeepEqual(loaded.CustomKVMImages, []CustomKVMImage{previous, image}) || !reflect.DeepEqual(loaded.EnabledImages, []string{previous.ID, image.ID}) {
		t.Fatalf("successful metadata and enabled flags did not both persist: %+v %v", loaded, err)
	}
	if err := AddUploadedKVMImage(image); err == nil || len(AppConfig.CustomKVMImages) != 2 || len(AppConfig.EnabledImages) != 2 {
		t.Fatal("duplicate image registration changed existing state")
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM enabled_images WHERE image_id = 'custom-kvm-new'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("success did not persist enabled flag: %d %v", count, err)
	}
}
