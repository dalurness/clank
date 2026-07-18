package vm

import (
	"reflect"
	"time"
)

// ── Channel ergonomics ──
// recv-opt, iter-recv (receiver → iterator bridge), and select-wait.

// chanRecvOptPtr blocks until a value arrives, the channel closes, or the
// context is cancelled. Returns (nil, nil) when the channel is closed and
// drained, and a non-nil error only for cancellation.
func (vm *VM) chanRecvOptPtr(ch *Channel) (*Value, error) {
	// Non-blocking drain first: buffered values are deliverable even after close.
	select {
	case v := <-ch.GoCh:
		return &v, nil
	default:
	}
	ch.mu.Lock()
	senderOpen := ch.SenderOpen
	ch.mu.Unlock()
	if !senderOpen {
		return nil, nil
	}
	select {
	case v, ok := <-ch.GoCh:
		if !ok {
			return nil, nil
		}
		return &v, nil
	case <-ch.Closed:
		// A value may have raced in just before the close.
		select {
		case v := <-ch.GoCh:
			return &v, nil
		default:
		}
		return nil, nil
	case <-vm.ctx.Done():
		return nil, vm.trap("E011", "task cancelled")
	}
}

// recv-opt(rx) -> Option[T]
// Blocking receive that returns Some(v), or None once the channel is
// closed and drained. The ergonomic way to drain a worker channel:
// no handler needed, just match until None.
func (vm *VM) builtinRecvOpt() error {
	rx, err := vm.pop()
	if err != nil {
		return err
	}
	if rx.Tag != TagHEAP || rx.Heap.Kind != KindReceiver {
		return vm.trap("E002", "recv-opt: expected Receiver")
	}
	v, recvErr := vm.chanRecvOptPtr(rx.Heap.Channel)
	if recvErr != nil {
		return recvErr
	}
	if v == nil {
		noneTag, _ := vm.findVariantTag("None")
		vm.push(ValUnion(noneTag, nil))
		return nil
	}
	someTag, _ := vm.findVariantTag("Some")
	vm.push(ValUnion(someTag, []Value{*v}))
	return nil
}

// receiverToIter wraps a channel receiver as a lazy iterator: each next()
// blocks for the next value and the iterator ends when the channel is
// closed and drained. This is what lets `for x in rx` and the iter.*
// combinators consume channels directly.
func (vm *VM) receiverToIter(rx Value) Value {
	ch := rx.Heap.Channel
	done := false
	return vm.newIter(func() *Value {
		if done {
			return nil
		}
		v, err := vm.chanRecvOptPtr(ch)
		if v == nil || err != nil {
			// On cancellation, end the iteration; the cancellation trap
			// surfaces at the next explicit check.
			done = true
			return nil
		}
		return v
	})
}

// iter-recv(rx) -> Iterator[T]
func (vm *VM) builtinIterRecv() error {
	rx, err := vm.pop()
	if err != nil {
		return err
	}
	if rx.Tag != TagHEAP || rx.Heap.Kind != KindReceiver {
		return vm.trap("E002", "iter-recv: expected Receiver")
	}
	vm.push(vm.receiverToIter(rx))
	return nil
}

// ── select-wait ──

// select-wait(arms) -> T
// arms is a list of (source, handler) tuples. A source is a channel
// Receiver (handler gets the received value), a Future (handler gets the
// awaited result), or an Int timeout in milliseconds (handler gets ()).
// Blocks until the first arm is ready and returns its handler's result.
func (vm *VM) opSelectWait(code []byte) error {
	setVal, _ := vm.pop()

	type liveArm struct {
		handler Value
		ch      *Channel   // non-nil for receiver arms
		task    *taskState // non-nil for future arms
	}
	var live []liveArm
	timeoutMs := -1
	var timeoutHandler Value

	var arms []SelectArm
	if setVal.Tag == TagHEAP && setVal.Heap.Kind == KindSelectSet {
		arms = setVal.Heap.SelectArms
	} else if setVal.Tag == TagHEAP && setVal.Heap.Kind == KindList {
		for _, item := range setVal.Heap.Items {
			if item.Tag != TagHEAP || item.Heap.Kind != KindTuple || len(item.Heap.Items) != 2 {
				return vm.trap("E002", "select-wait: expected list of (source, handler) tuples")
			}
			arms = append(arms, SelectArm{Source: item.Heap.Items[0], Handler: item.Heap.Items[1]})
		}
	} else {
		return vm.trap("E002", "select-wait: expected list of (source, handler) tuples")
	}
	if len(arms) == 0 {
		return vm.trap("E015", "select-wait: no arms")
	}

	root := vm.root()
	fire := func(handler Value, arg Value) error {
		result, err := vm.callBuiltinFn(handler, []Value{arg})
		if err != nil {
			return err
		}
		vm.push(result)
		return nil
	}
	fireFuture := func(handler Value, task *taskState) error {
		root.mu.Lock()
		status := task.status
		result := task.result
		errMsg := task.errMsg
		root.mu.Unlock()
		switch status {
		case "failed":
			return vm.raiseOrTrap("E014", errMsg)
		case "cancelled":
			if !vm.doRaise("awaited task was cancelled") {
				return vm.trap("E011", "awaited task was cancelled")
			}
			return nil
		}
		v := ValUnit()
		if result != nil {
			v = *result
		}
		return fire(handler, v)
	}

	for _, arm := range arms {
		src := arm.Source
		switch {
		case src.Tag == TagHEAP && src.Heap.Kind == KindReceiver:
			live = append(live, liveArm{handler: arm.Handler, ch: src.Heap.Channel})
		case src.Tag == TagHEAP && src.Heap.Kind == KindFuture:
			root.mu.Lock()
			task, ok := root.tasks[src.Heap.TaskID]
			root.mu.Unlock()
			if !ok {
				return vm.trap("E013", "select-wait: future references unknown task")
			}
			live = append(live, liveArm{handler: arm.Handler, task: task})
		case src.Tag == TagINT:
			if src.IntVal < 0 {
				return vm.trap("E002", "select-wait: negative timeout")
			}
			if timeoutMs < 0 || src.IntVal < timeoutMs {
				timeoutMs = src.IntVal
				timeoutHandler = arm.Handler
			}
		default:
			return vm.trap("E002", "select-wait: source must be a Receiver, Future, or timeout (Int ms)")
		}
	}

	// Ready-now pre-pass, in arm order: buffered channel values and
	// already-finished futures win over the timer without blocking.
	for _, la := range live {
		if la.ch != nil {
			select {
			case v := <-la.ch.GoCh:
				return fire(la.handler, v)
			default:
			}
		} else {
			root.mu.Lock()
			status := la.task.status
			root.mu.Unlock()
			if status != "running" && status != "suspended" {
				return fireFuture(la.handler, la.task)
			}
		}
	}

	var timerC <-chan time.Time
	if timeoutMs >= 0 {
		timer := time.NewTimer(time.Duration(timeoutMs) * time.Millisecond)
		defer timer.Stop()
		timerC = timer.C
	}

	for {
		// Case layout: per receiver arm, GoCh recv then Closed recv;
		// per future arm, resultCh recv; then timer; then ctx.Done.
		var cases []reflect.SelectCase
		var caseArm []int  // index into live
		var caseKind []int // 0 = value, 1 = closed, 2 = future, 3 = timer, 4 = ctx
		for i, la := range live {
			if la.ch != nil {
				cases = append(cases, reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(la.ch.GoCh)})
				caseArm = append(caseArm, i)
				caseKind = append(caseKind, 0)
				cases = append(cases, reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(la.ch.Closed)})
				caseArm = append(caseArm, i)
				caseKind = append(caseKind, 1)
			} else {
				cases = append(cases, reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(la.task.resultCh)})
				caseArm = append(caseArm, i)
				caseKind = append(caseKind, 2)
			}
		}
		if timerC != nil {
			cases = append(cases, reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(timerC)})
			caseArm = append(caseArm, -1)
			caseKind = append(caseKind, 3)
		}
		cases = append(cases, reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(vm.ctx.Done())})
		caseArm = append(caseArm, -1)
		caseKind = append(caseKind, 4)

		if len(live) == 0 && timerC == nil {
			return vm.raiseOrTrap("E012", "select-wait: all channels closed")
		}

		chosen, recvVal, _ := reflect.Select(cases)
		switch caseKind[chosen] {
		case 0: // channel value
			return fire(live[caseArm[chosen]].handler, recvVal.Interface().(Value))
		case 1: // channel closed
			la := live[caseArm[chosen]]
			select {
			case v := <-la.ch.GoCh: // late-buffered value beats the close
				return fire(la.handler, v)
			default:
			}
			live = append(live[:caseArm[chosen]], live[caseArm[chosen]+1:]...)
		case 2: // future completed
			la := live[caseArm[chosen]]
			// Put the result back so a later await on the same future
			// still wakes (resultCh has capacity 1, single send).
			if res, ok := recvVal.Interface().(taskResult); ok {
				select {
				case la.task.resultCh <- res:
				default:
				}
			}
			return fireFuture(la.handler, la.task)
		case 3: // timeout
			return fire(timeoutHandler, ValUnit())
		case 4: // cancelled
			return vm.trap("E011", "task cancelled")
		}
	}
}
