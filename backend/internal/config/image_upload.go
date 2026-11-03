package config

import "fmt"

// AddUploadedKVMImage publishes image metadata and its enabled flag together.
func AddUploadedKVMImage(image CustomKVMImage) error {
	allocationMu.Lock()
	defer allocationMu.Unlock()
	if AppConfig == nil {
		return fmt.Errorf("configuration is not loaded")
	}
	for _, existing := range AppConfig.CustomKVMImages {
		if existing.ID == image.ID {
			return fmt.Errorf("image already exists")
		}
	}
	previousImages := AppConfig.CustomKVMImages
	previousEnabled := AppConfig.EnabledImages
	AppConfig.CustomKVMImages = append(append([]CustomKVMImage(nil), previousImages...), image)
	AppConfig.EnabledImages = append(append([]string(nil), previousEnabled...), image.ID)
	if err := SaveConfig(); err != nil {
		AppConfig.CustomKVMImages = previousImages
		AppConfig.EnabledImages = previousEnabled
		return err
	}
	return nil
}
