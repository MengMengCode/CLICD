package api

import (
	"context"
	"errors"
	"testing"
	"time"

	"clicd/internal/config"
	"clicd/internal/lxc"
)

func TestIdleContainerRejectsPendingAndRunningTargets(t *testing.T) {
	setupTaskQueueConfig(t)
	config.AppConfig.Containers = []config.Container{{ID: 7, Name: "guest"}}
	for _, task := range []*Task{
		{ContainerID: 7, Type: TaskStart, Status: "pending"},
		{Config: lxc.ContainerConfig{Name: "GUEST"}, Type: TaskCreate, Status: "running"},
	} {
		q := newTaskQueue(1)
		q.tasks["busy"] = task
		called := false
		err := q.WithIdleContainer(7, func() error { called = true; return nil })
		if !errors.Is(err, ErrContainerBusy) || called {
			t.Fatalf("busy target callback ran: called=%v err=%v", called, err)
		}
	}
}

func TestIdleContainerReservationDelaysStartWithoutBlockingOtherTargets(t *testing.T) {
	setupTaskQueueConfig(t)
	config.AppConfig.Containers = []config.Container{{ID: 7, Name: "guest"}, {ID: 8, Name: "other"}}
	q := newTaskQueue(2)
	started := make(chan int, 2)
	q.runtime.start = func(_ context.Context, id int) error { started <- id; return nil }
	startTaskQueueDispatchers(t, q)
	entered, release, finished := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		finished <- q.WithIdleContainer(7, func() error { close(entered); <-release; return nil })
	}()
	waitTaskQueueSignal(t, entered, "media operation entered")
	if err := q.WithIdleContainer(7, func() error { t.Error("second media operation ran"); return nil }); !errors.Is(err, ErrContainerBusy) {
		t.Fatalf("concurrent media operation was accepted: %v", err)
	}
	q.Enqueue(7, "guest", TaskStart, "", nil)
	q.Enqueue(8, "other", TaskStart, "", nil)
	select {
	case id := <-started:
		if id != 8 {
			t.Fatalf("reserved guest started early: %d", id)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("unrelated guest was blocked by media operation")
	}
	close(release)
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	select {
	case id := <-started:
		if id != 7 {
			t.Fatalf("wrong guest resumed: %d", id)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("reserved guest did not resume after media operation")
	}
}

func TestIdleContainerReleasesReservationOnFailure(t *testing.T) {
	setupTaskQueueConfig(t)
	q := newTaskQueue(1)
	failure := errors.New("define failed")
	if err := q.WithIdleContainer(7, func() error { return failure }); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	called := false
	if err := q.WithIdleContainer(7, func() error { called = true; return nil }); err != nil || !called {
		t.Fatalf("failed operation left target reserved: %v", err)
	}
}
