package vm

// Iterator combinators — lazy sequence operations

// ── Iterator combinators ──

// extractIter validates and extracts an IteratorState from a Value.
func (vm *VM) extractIter(v Value) (*IteratorState, Value, error) {
	if v.Tag != TagHEAP || v.Heap.Kind != KindIterator {
		return nil, v, vm.trap("E002", "expected Iterator")
	}
	if v.Heap.Iter.Closed {
		return nil, v, vm.trap("E017", "iterator is closed")
	}
	return v.Heap.Iter, v, nil
}

// newIter creates a new IteratorState with a NativeNext function.
func (vm *VM) newIter(next func() *Value) Value {
	root := vm.root()
	root.mu.Lock()
	iter := &IteratorState{
		ID:          root.nextIterID,
		GeneratorFn: ValUnit(),
		CleanupFn:   ValUnit(),
		NativeNext:  next,
	}
	root.nextIterID++
	root.mu.Unlock()
	return ValIter(iter)
}

// ── Transforming (return new iterator) ──

func (vm *VM) builtinIterMap() error {
	fn, _ := vm.pop()
	iterVal, _ := vm.pop()
	srcIter, srcVal, err := vm.extractIter(iterVal)
	if err != nil {
		return err
	}
	vm.push(vm.newIter(func() *Value {
		v := vm.iterNext(srcIter, srcVal)
		if v == nil {
			return nil
		}
		result, callErr := vm.callBuiltinFn(fn, []Value{*v})
		if callErr != nil {
			return nil
		}
		return &result
	}))
	return nil
}

func (vm *VM) builtinIterFilter() error {
	fn, _ := vm.pop()
	iterVal, _ := vm.pop()
	srcIter, srcVal, err := vm.extractIter(iterVal)
	if err != nil {
		return err
	}
	vm.push(vm.newIter(func() *Value {
		for {
			v := vm.iterNext(srcIter, srcVal)
			if v == nil {
				return nil
			}
			result, callErr := vm.callBuiltinFn(fn, []Value{*v})
			if callErr != nil {
				return nil
			}
			if result.Tag == TagBOOL && result.BoolVal {
				return v
			}
		}
	}))
	return nil
}

func (vm *VM) builtinIterTake() error {
	nVal, _ := vm.pop()
	iterVal, _ := vm.pop()
	srcIter, srcVal, err := vm.extractIter(iterVal)
	if err != nil {
		return err
	}
	n := numVal(nVal)
	taken := 0
	vm.push(vm.newIter(func() *Value {
		if taken >= n {
			return nil
		}
		v := vm.iterNext(srcIter, srcVal)
		if v == nil {
			return nil
		}
		taken++
		return v
	}))
	return nil
}

func (vm *VM) builtinIterDrop() error {
	nVal, _ := vm.pop()
	iterVal, _ := vm.pop()
	srcIter, srcVal, err := vm.extractIter(iterVal)
	if err != nil {
		return err
	}
	n := numVal(nVal)
	dropped := false
	vm.push(vm.newIter(func() *Value {
		if !dropped {
			for i := 0; i < n; i++ {
				if vm.iterNext(srcIter, srcVal) == nil {
					return nil
				}
			}
			dropped = true
		}
		return vm.iterNext(srcIter, srcVal)
	}))
	return nil
}

func (vm *VM) builtinIterTakeWhile() error {
	fn, _ := vm.pop()
	iterVal, _ := vm.pop()
	srcIter, srcVal, err := vm.extractIter(iterVal)
	if err != nil {
		return err
	}
	done := false
	vm.push(vm.newIter(func() *Value {
		if done {
			return nil
		}
		v := vm.iterNext(srcIter, srcVal)
		if v == nil {
			return nil
		}
		result, callErr := vm.callBuiltinFn(fn, []Value{*v})
		if callErr != nil || (result.Tag == TagBOOL && !result.BoolVal) {
			done = true
			return nil
		}
		return v
	}))
	return nil
}

func (vm *VM) builtinIterDropWhile() error {
	fn, _ := vm.pop()
	iterVal, _ := vm.pop()
	srcIter, srcVal, err := vm.extractIter(iterVal)
	if err != nil {
		return err
	}
	dropping := true
	vm.push(vm.newIter(func() *Value {
		for dropping {
			v := vm.iterNext(srcIter, srcVal)
			if v == nil {
				return nil
			}
			result, callErr := vm.callBuiltinFn(fn, []Value{*v})
			if callErr != nil || (result.Tag == TagBOOL && !result.BoolVal) {
				dropping = false
				return v
			}
		}
		return vm.iterNext(srcIter, srcVal)
	}))
	return nil
}

func (vm *VM) builtinIterFlatMap() error {
	fn, _ := vm.pop()
	iterVal, _ := vm.pop()
	srcIter, srcVal, err := vm.extractIter(iterVal)
	if err != nil {
		return err
	}
	var inner []Value
	innerIdx := 0
	vm.push(vm.newIter(func() *Value {
		for {
			if innerIdx < len(inner) {
				v := inner[innerIdx]
				innerIdx++
				return &v
			}
			v := vm.iterNext(srcIter, srcVal)
			if v == nil {
				return nil
			}
			result, callErr := vm.callBuiltinFn(fn, []Value{*v})
			if callErr != nil {
				return nil
			}
			if result.Tag == TagHEAP && result.Heap.Kind == KindList {
				inner = result.Heap.Items
				innerIdx = 0
			} else {
				return &result
			}
		}
	}))
	return nil
}

func (vm *VM) builtinIterEnumerate() error {
	iterVal, _ := vm.pop()
	srcIter, srcVal, err := vm.extractIter(iterVal)
	if err != nil {
		return err
	}
	idx := 0
	vm.push(vm.newIter(func() *Value {
		v := vm.iterNext(srcIter, srcVal)
		if v == nil {
			return nil
		}
		t := ValTuple([]Value{ValInt(idx), *v})
		idx++
		return &t
	}))
	return nil
}

func (vm *VM) builtinIterChain() error {
	iter2Val, _ := vm.pop()
	iter1Val, _ := vm.pop()
	srcIter1, srcVal1, err := vm.extractIter(iter1Val)
	if err != nil {
		return err
	}
	srcIter2, srcVal2, err := vm.extractIter(iter2Val)
	if err != nil {
		return err
	}
	firstDone := false
	vm.push(vm.newIter(func() *Value {
		if !firstDone {
			v := vm.iterNext(srcIter1, srcVal1)
			if v != nil {
				return v
			}
			firstDone = true
		}
		return vm.iterNext(srcIter2, srcVal2)
	}))
	return nil
}

func (vm *VM) builtinIterZip() error {
	iter2Val, _ := vm.pop()
	iter1Val, _ := vm.pop()
	srcIter1, srcVal1, err := vm.extractIter(iter1Val)
	if err != nil {
		return err
	}
	srcIter2, srcVal2, err := vm.extractIter(iter2Val)
	if err != nil {
		return err
	}
	vm.push(vm.newIter(func() *Value {
		v1 := vm.iterNext(srcIter1, srcVal1)
		v2 := vm.iterNext(srcIter2, srcVal2)
		if v1 == nil || v2 == nil {
			return nil
		}
		t := ValTuple([]Value{*v1, *v2})
		return &t
	}))
	return nil
}

func (vm *VM) builtinIterScan() error {
	fn, _ := vm.pop()
	accVal, _ := vm.pop()
	iterVal, _ := vm.pop()
	srcIter, srcVal, err := vm.extractIter(iterVal)
	if err != nil {
		return err
	}
	acc := accVal
	vm.push(vm.newIter(func() *Value {
		v := vm.iterNext(srcIter, srcVal)
		if v == nil {
			return nil
		}
		result, callErr := vm.callBuiltinFn(fn, []Value{acc, *v})
		if callErr != nil {
			return nil
		}
		acc = result
		return &acc
	}))
	return nil
}

func (vm *VM) builtinIterDedup() error {
	iterVal, _ := vm.pop()
	srcIter, srcVal, err := vm.extractIter(iterVal)
	if err != nil {
		return err
	}
	var last *Value
	vm.push(vm.newIter(func() *Value {
		for {
			v := vm.iterNext(srcIter, srcVal)
			if v == nil {
				return nil
			}
			if last != nil && ValuesEqual(*last, *v) {
				continue
			}
			cp := *v
			last = &cp
			return v
		}
	}))
	return nil
}

func (vm *VM) builtinIterChunk() error {
	nVal, _ := vm.pop()
	iterVal, _ := vm.pop()
	srcIter, srcVal, err := vm.extractIter(iterVal)
	if err != nil {
		return err
	}
	n := numVal(nVal)
	if n <= 0 {
		return vm.trap("E002", "iter.chunk: chunk size must be > 0")
	}
	vm.push(vm.newIter(func() *Value {
		var chunk []Value
		for i := 0; i < n; i++ {
			v := vm.iterNext(srcIter, srcVal)
			if v == nil {
				break
			}
			chunk = append(chunk, *v)
		}
		if len(chunk) == 0 {
			return nil
		}
		result := ValList(chunk)
		return &result
	}))
	return nil
}

func (vm *VM) builtinIterWindow() error {
	nVal, _ := vm.pop()
	iterVal, _ := vm.pop()
	srcIter, srcVal, err := vm.extractIter(iterVal)
	if err != nil {
		return err
	}
	n := numVal(nVal)
	if n <= 0 {
		return vm.trap("E002", "iter.window: window size must be > 0")
	}
	buf := make([]Value, 0, n)
	filled := false
	vm.push(vm.newIter(func() *Value {
		if !filled {
			for len(buf) < n {
				v := vm.iterNext(srcIter, srcVal)
				if v == nil {
					return nil
				}
				buf = append(buf, *v)
			}
			filled = true
			win := make([]Value, n)
			copy(win, buf)
			result := ValList(win)
			return &result
		}
		v := vm.iterNext(srcIter, srcVal)
		if v == nil {
			return nil
		}
		buf = append(buf[1:], *v)
		win := make([]Value, n)
		copy(win, buf)
		result := ValList(win)
		return &result
	}))
	return nil
}

func (vm *VM) builtinIterIntersperse() error {
	sep, _ := vm.pop()
	iterVal, _ := vm.pop()
	srcIter, srcVal, err := vm.extractIter(iterVal)
	if err != nil {
		return err
	}
	first := true
	var nextVal *Value
	vm.push(vm.newIter(func() *Value {
		if nextVal != nil {
			v := *nextVal
			nextVal = nil
			return &v
		}
		v := vm.iterNext(srcIter, srcVal)
		if v == nil {
			return nil
		}
		if first {
			first = false
			return v
		}
		nextVal = v
		s := sep
		return &s
	}))
	return nil
}

func (vm *VM) builtinIterCycle() error {
	iterVal, _ := vm.pop()
	srcIter, srcVal, err := vm.extractIter(iterVal)
	if err != nil {
		return err
	}
	var collected []Value
	collecting := true
	cycleIdx := 0
	vm.push(vm.newIter(func() *Value {
		if collecting {
			v := vm.iterNext(srcIter, srcVal)
			if v != nil {
				collected = append(collected, *v)
				return v
			}
			collecting = false
			if len(collected) == 0 {
				return nil
			}
		}
		v := collected[cycleIdx%len(collected)]
		cycleIdx++
		return &v
	}))
	return nil
}

// ── Consuming (return value) ──

func (vm *VM) builtinIterFold() error {
	fn, _ := vm.pop()
	acc, _ := vm.pop()
	iterVal, _ := vm.pop()
	srcIter, srcVal, err := vm.extractIter(iterVal)
	if err != nil {
		return err
	}
	for {
		v := vm.iterNext(srcIter, srcVal)
		if v == nil {
			break
		}
		result, callErr := vm.callBuiltinFn(fn, []Value{acc, *v})
		if callErr != nil {
			return callErr
		}
		acc = result
	}
	vm.push(acc)
	return nil
}

func (vm *VM) builtinIterCount() error {
	iterVal, _ := vm.pop()
	srcIter, srcVal, err := vm.extractIter(iterVal)
	if err != nil {
		return err
	}
	count := 0
	for vm.iterNext(srcIter, srcVal) != nil {
		count++
	}
	vm.push(ValInt(count))
	return nil
}

func (vm *VM) builtinIterSum() error {
	iterVal, _ := vm.pop()
	srcIter, srcVal, err := vm.extractIter(iterVal)
	if err != nil {
		return err
	}
	sum := 0
	for {
		v := vm.iterNext(srcIter, srcVal)
		if v == nil {
			break
		}
		sum += numVal(*v)
	}
	vm.push(ValInt(sum))
	return nil
}

func (vm *VM) builtinIterAny() error {
	fn, _ := vm.pop()
	iterVal, _ := vm.pop()
	srcIter, srcVal, err := vm.extractIter(iterVal)
	if err != nil {
		return err
	}
	for {
		v := vm.iterNext(srcIter, srcVal)
		if v == nil {
			break
		}
		result, callErr := vm.callBuiltinFn(fn, []Value{*v})
		if callErr != nil {
			return callErr
		}
		if result.Tag == TagBOOL && result.BoolVal {
			vm.push(ValBool(true))
			return nil
		}
	}
	vm.push(ValBool(false))
	return nil
}

func (vm *VM) builtinIterAll() error {
	fn, _ := vm.pop()
	iterVal, _ := vm.pop()
	srcIter, srcVal, err := vm.extractIter(iterVal)
	if err != nil {
		return err
	}
	for {
		v := vm.iterNext(srcIter, srcVal)
		if v == nil {
			break
		}
		result, callErr := vm.callBuiltinFn(fn, []Value{*v})
		if callErr != nil {
			return callErr
		}
		if result.Tag == TagBOOL && !result.BoolVal {
			vm.push(ValBool(false))
			return nil
		}
	}
	vm.push(ValBool(true))
	return nil
}

func (vm *VM) builtinIterFind() error {
	fn, _ := vm.pop()
	iterVal, _ := vm.pop()
	srcIter, srcVal, err := vm.extractIter(iterVal)
	if err != nil {
		return err
	}
	someTag, err := vm.findVariantTag("Some")
	if err != nil {
		return err
	}
	noneTag, err := vm.findVariantTag("None")
	if err != nil {
		return err
	}
	for {
		v := vm.iterNext(srcIter, srcVal)
		if v == nil {
			break
		}
		result, callErr := vm.callBuiltinFn(fn, []Value{*v})
		if callErr != nil {
			return callErr
		}
		if result.Tag == TagBOOL && result.BoolVal {
			vm.push(ValUnion(someTag, []Value{*v}))
			return nil
		}
	}
	vm.push(ValUnion(noneTag, nil))
	return nil
}

func (vm *VM) builtinIterEach() error {
	fn, _ := vm.pop()
	iterVal, _ := vm.pop()
	srcIter, srcVal, err := vm.extractIter(iterVal)
	if err != nil {
		return err
	}
	for {
		v := vm.iterNext(srcIter, srcVal)
		if v == nil {
			break
		}
		if _, callErr := vm.callBuiltinFn(fn, []Value{*v}); callErr != nil {
			return callErr
		}
	}
	vm.push(ValUnit())
	return nil
}

func (vm *VM) builtinIterFirst() error {
	iterVal, _ := vm.pop()
	srcIter, srcVal, err := vm.extractIter(iterVal)
	if err != nil {
		return err
	}
	someTag, err := vm.findVariantTag("Some")
	if err != nil {
		return err
	}
	noneTag, err := vm.findVariantTag("None")
	if err != nil {
		return err
	}
	v := vm.iterNext(srcIter, srcVal)
	if v == nil {
		vm.push(ValUnion(noneTag, nil))
	} else {
		vm.push(ValUnion(someTag, []Value{*v}))
	}
	return nil
}

func (vm *VM) builtinIterLast() error {
	iterVal, _ := vm.pop()
	srcIter, srcVal, err := vm.extractIter(iterVal)
	if err != nil {
		return err
	}
	someTag, err := vm.findVariantTag("Some")
	if err != nil {
		return err
	}
	noneTag, err := vm.findVariantTag("None")
	if err != nil {
		return err
	}
	var last *Value
	for {
		v := vm.iterNext(srcIter, srcVal)
		if v == nil {
			break
		}
		cp := *v
		last = &cp
	}
	if last == nil {
		vm.push(ValUnion(noneTag, nil))
	} else {
		vm.push(ValUnion(someTag, []Value{*last}))
	}
	return nil
}

func (vm *VM) builtinIterJoin() error {
	sepVal, _ := vm.pop()
	iterVal, _ := vm.pop()
	srcIter, srcVal, err := vm.extractIter(iterVal)
	if err != nil {
		return err
	}
	sep := ""
	if sepVal.Tag == TagSTR {
		sep = sepVal.StrVal
	}
	var parts []string
	for {
		v := vm.iterNext(srcIter, srcVal)
		if v == nil {
			break
		}
		parts = append(parts, valShow(*v))
	}
	result := ""
	for i, p := range parts {
		if i > 0 {
			result += sep
		}
		result += p
	}
	vm.push(ValStr(result))
	return nil
}

func (vm *VM) builtinIterNth() error {
	nVal, _ := vm.pop()
	iterVal, _ := vm.pop()
	srcIter, srcVal, err := vm.extractIter(iterVal)
	if err != nil {
		return err
	}
	n := numVal(nVal)
	someTag, err := vm.findVariantTag("Some")
	if err != nil {
		return err
	}
	noneTag, err := vm.findVariantTag("None")
	if err != nil {
		return err
	}
	for i := 0; i <= n; i++ {
		v := vm.iterNext(srcIter, srcVal)
		if v == nil {
			vm.push(ValUnion(noneTag, nil))
			return nil
		}
		if i == n {
			vm.push(ValUnion(someTag, []Value{*v}))
			return nil
		}
	}
	vm.push(ValUnion(noneTag, nil))
	return nil
}

func (vm *VM) builtinIterMin() error {
	iterVal, _ := vm.pop()
	srcIter, srcVal, err := vm.extractIter(iterVal)
	if err != nil {
		return err
	}
	someTag, err := vm.findVariantTag("Some")
	if err != nil {
		return err
	}
	noneTag, err := vm.findVariantTag("None")
	if err != nil {
		return err
	}
	var min *Value
	for {
		v := vm.iterNext(srcIter, srcVal)
		if v == nil {
			break
		}
		if min == nil || valLess(*v, *min) {
			cp := *v
			min = &cp
		}
	}
	if min == nil {
		vm.push(ValUnion(noneTag, nil))
	} else {
		vm.push(ValUnion(someTag, []Value{*min}))
	}
	return nil
}

func (vm *VM) builtinIterMax() error {
	iterVal, _ := vm.pop()
	srcIter, srcVal, err := vm.extractIter(iterVal)
	if err != nil {
		return err
	}
	someTag, err := vm.findVariantTag("Some")
	if err != nil {
		return err
	}
	noneTag, err := vm.findVariantTag("None")
	if err != nil {
		return err
	}
	var max *Value
	for {
		v := vm.iterNext(srcIter, srcVal)
		if v == nil {
			break
		}
		if max == nil || valLess(*max, *v) {
			cp := *v
			max = &cp
		}
	}
	if max == nil {
		vm.push(ValUnion(noneTag, nil))
	} else {
		vm.push(ValUnion(someTag, []Value{*max}))
	}
	return nil
}

// ── Creating (return new iterator) ──

func (vm *VM) builtinIterRepeat() error {
	val, _ := vm.pop()
	vm.push(vm.newIter(func() *Value {
		v := val
		return &v
	}))
	return nil
}

func (vm *VM) builtinIterOnce() error {
	val, _ := vm.pop()
	done := false
	vm.push(vm.newIter(func() *Value {
		if done {
			return nil
		}
		done = true
		v := val
		return &v
	}))
	return nil
}

func (vm *VM) builtinIterEmpty() error {
	vm.push(vm.newIter(func() *Value {
		return nil
	}))
	return nil
}

func (vm *VM) builtinIterUnfold() error {
	fn, _ := vm.pop()
	state, _ := vm.pop()
	current := state
	vm.push(vm.newIter(func() *Value {
		result, callErr := vm.callBuiltinFn(fn, []Value{current})
		if callErr != nil {
			return nil
		}
		// Expect Option[(value, newState)] — Some((v, s)) or None
		if result.Tag == TagHEAP && result.Heap.Kind == KindUnion {
			if result.Heap.VariantTag < len(vm.variantNames) {
				name := vm.variantNames[result.Heap.VariantTag]
				if name == "None" {
					return nil
				}
				if name == "Some" && len(result.Heap.UFields) > 0 {
					pair := result.Heap.UFields[0]
					if pair.Tag == TagHEAP && pair.Heap.Kind == KindTuple && len(pair.Heap.Items) >= 2 {
						val := pair.Heap.Items[0]
						current = pair.Heap.Items[1]
						return &val
					}
				}
			}
		}
		return nil
	}))
	return nil
}

func (vm *VM) builtinIterGenerate() error {
	fn, _ := vm.pop()
	vm.push(vm.newIter(func() *Value {
		result, callErr := vm.callBuiltinFn(fn, []Value{ValUnit()})
		if callErr != nil {
			return nil
		}
		// Expect Option[value] — Some(v) or None
		if result.Tag == TagHEAP && result.Heap.Kind == KindUnion {
			if result.Heap.VariantTag < len(vm.variantNames) {
				name := vm.variantNames[result.Heap.VariantTag]
				if name == "None" {
					return nil
				}
				if name == "Some" && len(result.Heap.UFields) > 0 {
					v := result.Heap.UFields[0]
					return &v
				}
			}
		}
		return nil
	}))
	return nil
}
