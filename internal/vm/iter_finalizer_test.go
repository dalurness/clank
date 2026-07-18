package vm

import (
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dalurness/clank/internal/compiler"
)

// A stream iterator that is created and then simply dropped (never drained,
// never short-circuited) must still release its resource once the GC
// collects it, instead of leaking until process exit.
func TestAbandonedIteratorFinalizerReleasesResource(t *testing.T) {
	vm := New(&compiler.BytecodeModule{})
	var released atomic.Bool

	// Create in a separate scope-function so the Value doesn't stay
	// reachable through this frame.
	func() {
		_ = vm.newIterWithCleanup(
			func() *Value { return nil },
			func() { released.Store(true) },
		)
	}()

	deadline := time.Now().Add(5 * time.Second)
	for !released.Load() && time.Now().Before(deadline) {
		runtime.GC()
		time.Sleep(10 * time.Millisecond)
	}
	if !released.Load() {
		t.Fatal("abandoned iterator's cleanup never ran after GC")
	}
}

// The finalizer must be a no-op when the resource was already released
// through the normal exhaustion/short-circuit path.
func TestReleaseResourcesIdempotentWithFinalizer(t *testing.T) {
	vm := New(&compiler.BytecodeModule{})
	var count atomic.Int32
	v := vm.newIterWithCleanup(
		func() *Value { return nil },
		func() { count.Add(1) },
	)
	v.Heap.Iter.ReleaseResources()
	v.Heap.Iter.ReleaseResources()
	runtime.GC()
	time.Sleep(50 * time.Millisecond)
	runtime.GC()
	time.Sleep(50 * time.Millisecond)
	if got := count.Load(); got != 1 {
		t.Fatalf("cleanup ran %d times, expected exactly 1", got)
	}
}
