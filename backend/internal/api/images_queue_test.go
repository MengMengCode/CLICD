package api

import (
	"context"
	"testing"
	"time"

	"clicd/internal/lxc"
)

func resetLXCQueueForTest() {
	lxcDownloadQueueMu.Lock()
	lxcDownloadQueue = nil
	lxcActiveDownload = nil
	lxcDownloadQueueMu.Unlock()

	imageDownloadsMu.Lock()
	imageDownloads = map[string]*imageDownloadStatus{}
	imageDownloadsMu.Unlock()
}

func TestLXCImageDownloadQueue_SequentialEnqueue(t *testing.T) {
	resetLXCQueueForTest()
	defer resetLXCQueueForTest()

	tmpl1 := lxc.Template{ID: "test-lxc-queue-1", Distro: "alpine", Release: "3.21", Arch: "amd64"}
	tmpl2 := lxc.Template{ID: "test-lxc-queue-2", Distro: "debian", Release: "12", Arch: "amd64"}
	tmpl3 := lxc.Template{ID: "test-lxc-queue-3", Distro: "ubuntu", Release: "noble", Arch: "amd64"}

	// 1. Manually set a mock active download so executeLXCImageDownload is not running external lxc-create
	lxcDownloadQueueMu.Lock()
	ctx1, cancel1 := context.WithCancel(context.Background())
	defer cancel1()
	task1 := &lxcImageDownloadTask{template: tmpl1, ctx: ctx1, cancel: cancel1}
	lxcActiveDownload = task1
	imageDownloadsMu.Lock()
	imageDownloads[tmpl1.ID] = &imageDownloadStatus{
		Downloading: true,
		Stage:       "lxc-create",
		Cancel:      cancel1,
		UpdatedAt:   time.Now(),
	}
	imageDownloadsMu.Unlock()
	lxcDownloadQueueMu.Unlock()

	// 2. Enqueue tmpl2 - should be queued
	queued2, ok2 := enqueueLXCImageDownload(tmpl2)
	if !ok2 {
		t.Fatalf("expected tmpl2 to be enqueued successfully")
	}
	if !queued2 {
		t.Fatalf("expected tmpl2 to be queued (queued=true), got queued=false")
	}

	st2 := imageDownloadInfo(tmpl2.ID)
	if !st2.Downloading || st2.Stage != "queued" {
		t.Fatalf("expected tmpl2 status to be downloading with stage 'queued', got %+v", st2)
	}

	// 3. Enqueue tmpl3 - should also be queued
	queued3, ok3 := enqueueLXCImageDownload(tmpl3)
	if !ok3 || !queued3 {
		t.Fatalf("expected tmpl3 to be queued, ok=%v, queued=%v", ok3, queued3)
	}

	st3 := imageDownloadInfo(tmpl3.ID)
	if !st3.Downloading || st3.Stage != "queued" {
		t.Fatalf("expected tmpl3 status to be downloading with stage 'queued', got %+v", st3)
	}

	// 4. Try re-enqueuing tmpl2 or tmpl1 - should be rejected as already active or queued
	_, okDup1 := enqueueLXCImageDownload(tmpl1)
	if okDup1 {
		t.Fatalf("expected re-enqueuing active tmpl1 to fail")
	}
	_, okDup2 := enqueueLXCImageDownload(tmpl2)
	if okDup2 {
		t.Fatalf("expected re-enqueuing queued tmpl2 to fail")
	}

	// Verify queue length
	lxcDownloadQueueMu.Lock()
	if len(lxcDownloadQueue) != 2 {
		t.Fatalf("expected 2 items in queue, got %d", len(lxcDownloadQueue))
	}
	if lxcDownloadQueue[0].template.ID != tmpl2.ID || lxcDownloadQueue[1].template.ID != tmpl3.ID {
		t.Fatalf("queue order incorrect: [0]=%s, [1]=%s", lxcDownloadQueue[0].template.ID, lxcDownloadQueue[1].template.ID)
	}
	lxcDownloadQueueMu.Unlock()
}

func TestLXCImageDownloadQueue_CancelQueued(t *testing.T) {
	resetLXCQueueForTest()
	defer resetLXCQueueForTest()

	tmpl1 := lxc.Template{ID: "test-cancel-active", Distro: "alpine", Release: "3.21"}
	tmpl2 := lxc.Template{ID: "test-cancel-queued", Distro: "debian", Release: "12"}
	tmpl3 := lxc.Template{ID: "test-cancel-queued-2", Distro: "ubuntu", Release: "noble"}

	// Set active
	lxcDownloadQueueMu.Lock()
	ctx1, cancel1 := context.WithCancel(context.Background())
	defer cancel1()
	lxcActiveDownload = &lxcImageDownloadTask{template: tmpl1, ctx: ctx1, cancel: cancel1}
	imageDownloadsMu.Lock()
	imageDownloads[tmpl1.ID] = &imageDownloadStatus{Downloading: true, Stage: "lxc-create", Cancel: cancel1}
	imageDownloadsMu.Unlock()
	lxcDownloadQueueMu.Unlock()

	// Enqueue tmpl2 and tmpl3
	enqueueLXCImageDownload(tmpl2)
	enqueueLXCImageDownload(tmpl3)

	// Cancel tmpl2 while in queue
	lxcDownloadQueueMu.Lock()
	var removed bool
	for i, task := range lxcDownloadQueue {
		if task.template.ID == tmpl2.ID {
			task.cancel()
			lxcDownloadQueue = append(lxcDownloadQueue[:i], lxcDownloadQueue[i+1:]...)
			removed = true
			break
		}
	}
	lxcDownloadQueueMu.Unlock()
	finishImageDownload(tmpl2.ID, nil)

	if !removed {
		t.Fatalf("expected tmpl2 to be removed from queue")
	}

	st2 := imageDownloadInfo(tmpl2.ID)
	if st2.Downloading {
		t.Fatalf("expected tmpl2 to not be downloading after cancel")
	}

	// tmpl3 should still be in queue
	lxcDownloadQueueMu.Lock()
	if len(lxcDownloadQueue) != 1 || lxcDownloadQueue[0].template.ID != tmpl3.ID {
		t.Fatalf("expected tmpl3 to remain in queue, len=%d", len(lxcDownloadQueue))
	}
	lxcDownloadQueueMu.Unlock()
}
