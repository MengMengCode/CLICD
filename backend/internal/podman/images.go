package podman

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"
)

type Image struct {
	ID        string    `json:"id"`
	Tags      []string  `json:"tags"`
	SizeBytes int64     `json:"size_bytes"`
	CreatedAt time.Time `json:"created_at"`
}

func (Manager) Images(ctx context.Context) ([]Image, error) {
	out, err := run(ctx, quickTimeout, "image", "list", "--no-trunc", "--format", "json")
	if err != nil {
		return nil, err
	}
	return parseImageList(out)
}

func (Manager) PullImage(ctx context.Context, reference string) (string, error) {
	reference = strings.TrimSpace(reference)
	if !validImage(reference) {
		return "", fmt.Errorf("%w: use a fully qualified image such as docker.io/library/nginx:alpine", ErrInvalid)
	}
	out, err := run(ctx, createTimeout, "pull", "--quiet", "--", reference)
	if err != nil {
		return "", err
	}
	id := imageID(strings.TrimSpace(string(out)))
	if id == "" {
		return "", fmt.Errorf("Podman did not return the pulled image's ID")
	}
	return id, nil
}

func imageID(value string) string {
	id := strings.TrimPrefix(value, "sha256:")
	if fullID.MatchString(id) {
		return id
	}
	return ""
}

func parseImageList(data []byte) ([]Image, error) {
	var rows []struct {
		ID        string    `json:"Id"`
		RepoTags  []string  `json:"RepoTags"`
		Names     []string  `json:"Names"`
		Size      int64     `json:"Size"`
		Created   int64     `json:"Created"`
		CreatedAt time.Time `json:"CreatedAt"`
	}
	if err := json.Unmarshal(data, &rows); err != nil {
		return nil, fmt.Errorf("invalid Podman image list: %w", err)
	}

	images := make([]Image, 0, len(rows))
	index := make(map[string]int, len(rows))
	for _, row := range rows {
		id := imageID(row.ID)
		if id == "" {
			continue
		}
		i, found := index[id]
		if !found {
			i = len(images)
			index[id] = i
			created := row.CreatedAt
			if created.IsZero() && row.Created > 0 {
				created = time.Unix(row.Created, 0).UTC()
			}
			images = append(images, Image{ID: id, Tags: []string{}, SizeBytes: max(row.Size, 0), CreatedAt: created})
		}
		for _, tag := range append(row.Names, row.RepoTags...) {
			if validImage(tag) {
				images[i].Tags = append(images[i].Tags, tag)
			}
		}
	}
	for i := range images {
		sort.Strings(images[i].Tags)
		images[i].Tags = slices.Compact(images[i].Tags)
	}
	sort.Slice(images, func(i, j int) bool {
		if images[i].CreatedAt.Equal(images[j].CreatedAt) {
			return images[i].ID < images[j].ID
		}
		return images[i].CreatedAt.After(images[j].CreatedAt)
	})
	return images, nil
}
