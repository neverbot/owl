package config

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestManagerSnapshotReflectsInitial(t *testing.T) {
	c := Default()
	c.Listen = "127.0.0.1:1111"

	m := NewManager(c)
	got := m.Snapshot()
	if got.Listen != "127.0.0.1:1111" {
		t.Errorf("Snapshot().Listen = %q", got.Listen)
	}
}

func TestManagerSwapReplacesSnapshotAndNotifiesSubscribers(t *testing.T) {
	m := NewManager(Default())
	ch := m.Subscribe()

	newCfg := Default()
	newCfg.Listen = "0.0.0.0:1234"
	m.Swap(newCfg)

	if got := m.Snapshot().Listen; got != "0.0.0.0:1234" {
		t.Errorf("Snapshot().Listen after Swap = %q", got)
	}

	select {
	case <-ch:
		// good
	case <-time.After(100 * time.Millisecond):
		t.Fatal("subscriber was not notified after Swap")
	}
}

func TestManagerSwapNotifiesMultipleSubscribersAndIsNonBlockingForSlowOnes(t *testing.T) {
	m := NewManager(Default())
	chSlow := m.Subscribe() // never drained
	chFast := m.Subscribe()

	var notified int32
	go func() {
		<-chFast
		atomic.StoreInt32(&notified, 1)
	}()

	m.Swap(Default())
	m.Swap(Default()) // a second swap must not deadlock on the slow subscriber

	time.Sleep(50 * time.Millisecond)
	if atomic.LoadInt32(&notified) == 0 {
		t.Error("fast subscriber not notified")
	}
	_ = chSlow
}

func TestManagerConcurrentSnapshotsAreSafe(t *testing.T) {
	m := NewManager(Default())
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = m.Snapshot()
			}
		}()
	}
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				m.Swap(Default())
			}
		}()
	}
	wg.Wait()
}
