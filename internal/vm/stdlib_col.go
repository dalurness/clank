package vm

import (
	"fmt"
	"sort"
)

// ── std.col — Collection operations ──

func (vm *VM) builtinColRev() error {
	list, err := vm.popList()
	if err != nil {
		return err
	}
	result := make([]Value, len(list))
	for i, v := range list {
		result[len(list)-1-i] = v
	}
	vm.push(ValList(result))
	return nil
}

func (vm *VM) builtinColSort() error {
	list, err := vm.popList()
	if err != nil {
		return err
	}
	result := make([]Value, len(list))
	copy(result, list)
	sort.SliceStable(result, func(i, j int) bool {
		a, b := result[i], result[j]
		if a.Tag == TagSTR && b.Tag == TagSTR {
			return a.StrVal < b.StrVal
		}
		av, _ := NumericValue(a)
		bv, _ := NumericValue(b)
		return av < bv
	})
	vm.push(ValList(result))
	return nil
}

func (vm *VM) builtinColSortBy() error {
	fn, err := vm.pop()
	if err != nil {
		return err
	}
	list, err := vm.popList()
	if err != nil {
		return err
	}
	result := make([]Value, len(list))
	copy(result, list)
	var sortErr error
	sort.SliceStable(result, func(i, j int) bool {
		if sortErr != nil {
			return false
		}
		res, callErr := vm.callBuiltinFn(fn, []Value{result[i], result[j]})
		if callErr != nil {
			sortErr = callErr
			return false
		}
		return numVal(res) < 0
	})
	if sortErr != nil {
		return sortErr
	}
	vm.push(ValList(result))
	return nil
}

func (vm *VM) builtinColUniq() error {
	list, err := vm.popList()
	if err != nil {
		return err
	}
	if len(list) == 0 {
		vm.push(ValList(nil))
		return nil
	}
	result := []Value{list[0]}
	for i := 1; i < len(list); i++ {
		if !ValuesEqual(list[i], list[i-1]) {
			result = append(result, list[i])
		}
	}
	vm.push(ValList(result))
	return nil
}

func (vm *VM) builtinColZip() error {
	b, err := vm.popList()
	if err != nil {
		return err
	}
	a, err := vm.popList()
	if err != nil {
		return err
	}
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	result := make([]Value, n)
	for i := 0; i < n; i++ {
		result[i] = ValTuple([]Value{a[i], b[i]})
	}
	vm.push(ValList(result))
	return nil
}

func (vm *VM) builtinColUnzip() error {
	list, err := vm.popList()
	if err != nil {
		return err
	}
	as := make([]Value, len(list))
	bs := make([]Value, len(list))
	for i, v := range list {
		if v.Tag != TagHEAP || v.Heap.Kind != KindTuple || len(v.Heap.Items) < 2 {
			return vm.trap("E002", "col.unzip: expected list of tuples")
		}
		as[i] = v.Heap.Items[0]
		bs[i] = v.Heap.Items[1]
	}
	vm.push(ValTuple([]Value{ValList(as), ValList(bs)}))
	return nil
}

func (vm *VM) builtinColFlat() error {
	list, err := vm.popList()
	if err != nil {
		return err
	}
	var result []Value
	for _, v := range list {
		if v.Tag == TagHEAP && v.Heap.Kind == KindList {
			result = append(result, v.Heap.Items...)
		} else {
			result = append(result, v)
		}
	}
	vm.push(ValList(result))
	return nil
}

func (vm *VM) builtinColFlatMap() error {
	fn, err := vm.pop()
	if err != nil {
		return err
	}
	list, err := vm.popList()
	if err != nil {
		return err
	}
	var result []Value
	for _, v := range list {
		res, callErr := vm.callBuiltinFn(fn, []Value{v})
		if callErr != nil {
			return callErr
		}
		if res.Tag == TagHEAP && res.Heap.Kind == KindList {
			result = append(result, res.Heap.Items...)
		} else {
			result = append(result, res)
		}
	}
	vm.push(ValList(result))
	return nil
}

func (vm *VM) builtinColTake() error {
	nVal, err := vm.pop()
	if err != nil {
		return err
	}
	list, err := vm.popList()
	if err != nil {
		return err
	}
	n := numVal(nVal)
	if n < 0 {
		n = 0
	}
	if n > len(list) {
		n = len(list)
	}
	result := make([]Value, n)
	copy(result, list[:n])
	vm.push(ValList(result))
	return nil
}

func (vm *VM) builtinColDrop() error {
	nVal, err := vm.pop()
	if err != nil {
		return err
	}
	list, err := vm.popList()
	if err != nil {
		return err
	}
	n := numVal(nVal)
	if n < 0 {
		n = 0
	}
	if n > len(list) {
		n = len(list)
	}
	result := make([]Value, len(list)-n)
	copy(result, list[n:])
	vm.push(ValList(result))
	return nil
}

func (vm *VM) builtinColNth() error {
	nVal, err := vm.pop()
	if err != nil {
		return err
	}
	list, err := vm.popList()
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
	if n < 0 || n >= len(list) {
		vm.push(ValUnion(noneTag, nil))
	} else {
		vm.push(ValUnion(someTag, []Value{list[n]}))
	}
	return nil
}

func (vm *VM) builtinColFind() error {
	fn, err := vm.pop()
	if err != nil {
		return err
	}
	list, err := vm.popList()
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
	for _, v := range list {
		res, callErr := vm.callBuiltinFn(fn, []Value{v})
		if callErr != nil {
			return callErr
		}
		if res.Tag == TagBOOL && res.BoolVal {
			vm.push(ValUnion(someTag, []Value{v}))
			return nil
		}
	}
	vm.push(ValUnion(noneTag, nil))
	return nil
}

func (vm *VM) builtinColAny() error {
	fn, err := vm.pop()
	if err != nil {
		return err
	}
	list, err := vm.popList()
	if err != nil {
		return err
	}
	for _, v := range list {
		res, callErr := vm.callBuiltinFn(fn, []Value{v})
		if callErr != nil {
			return callErr
		}
		if res.Tag == TagBOOL && res.BoolVal {
			vm.push(ValBool(true))
			return nil
		}
	}
	vm.push(ValBool(false))
	return nil
}

func (vm *VM) builtinColAll() error {
	fn, err := vm.pop()
	if err != nil {
		return err
	}
	list, err := vm.popList()
	if err != nil {
		return err
	}
	for _, v := range list {
		res, callErr := vm.callBuiltinFn(fn, []Value{v})
		if callErr != nil {
			return callErr
		}
		if res.Tag == TagBOOL && !res.BoolVal {
			vm.push(ValBool(false))
			return nil
		}
	}
	vm.push(ValBool(true))
	return nil
}

func (vm *VM) builtinColCount() error {
	fn, err := vm.pop()
	if err != nil {
		return err
	}
	list, err := vm.popList()
	if err != nil {
		return err
	}
	count := 0
	for _, v := range list {
		res, callErr := vm.callBuiltinFn(fn, []Value{v})
		if callErr != nil {
			return callErr
		}
		if res.Tag == TagBOOL && res.BoolVal {
			count++
		}
	}
	vm.push(ValInt(count))
	return nil
}

func (vm *VM) builtinColEnum() error {
	list, err := vm.popList()
	if err != nil {
		return err
	}
	result := make([]Value, len(list))
	for i, v := range list {
		result[i] = ValTuple([]Value{ValInt(i), v})
	}
	vm.push(ValList(result))
	return nil
}

func (vm *VM) builtinColChunk() error {
	nVal, err := vm.pop()
	if err != nil {
		return err
	}
	list, err := vm.popList()
	if err != nil {
		return err
	}
	n := numVal(nVal)
	if n <= 0 {
		return vm.trap("E002", "col.chunk: chunk size must be > 0")
	}
	var result []Value
	for i := 0; i < len(list); i += n {
		end := i + n
		if end > len(list) {
			end = len(list)
		}
		chunk := make([]Value, end-i)
		copy(chunk, list[i:end])
		result = append(result, ValList(chunk))
	}
	vm.push(ValList(result))
	return nil
}

func (vm *VM) builtinColWin() error {
	nVal, err := vm.pop()
	if err != nil {
		return err
	}
	list, err := vm.popList()
	if err != nil {
		return err
	}
	n := numVal(nVal)
	if n <= 0 {
		return vm.trap("E002", "col.win: window size must be > 0")
	}
	var result []Value
	for i := 0; i <= len(list)-n; i++ {
		win := make([]Value, n)
		copy(win, list[i:i+n])
		result = append(result, ValList(win))
	}
	vm.push(ValList(result))
	return nil
}

func (vm *VM) builtinColIntersperse() error {
	sep, err := vm.pop()
	if err != nil {
		return err
	}
	list, err := vm.popList()
	if err != nil {
		return err
	}
	if len(list) <= 1 {
		result := make([]Value, len(list))
		copy(result, list)
		vm.push(ValList(result))
		return nil
	}
	result := make([]Value, 0, len(list)*2-1)
	for i, v := range list {
		if i > 0 {
			result = append(result, sep)
		}
		result = append(result, v)
	}
	vm.push(ValList(result))
	return nil
}

func (vm *VM) builtinColRep() error {
	nVal, err := vm.pop()
	if err != nil {
		return err
	}
	val, err := vm.pop()
	if err != nil {
		return err
	}
	n := numVal(nVal)
	if n < 0 {
		n = 0
	}
	result := make([]Value, n)
	for i := range result {
		result[i] = val
	}
	vm.push(ValList(result))
	return nil
}

func (vm *VM) builtinColSum() error {
	list, err := vm.popList()
	if err != nil {
		return err
	}
	sum := 0
	for _, v := range list {
		sum += numVal(v)
	}
	vm.push(ValInt(sum))
	return nil
}

func (vm *VM) builtinColProd() error {
	list, err := vm.popList()
	if err != nil {
		return err
	}
	prod := 1
	for _, v := range list {
		prod *= numVal(v)
	}
	vm.push(ValInt(prod))
	return nil
}

func (vm *VM) builtinColMin() error {
	list, err := vm.popList()
	if err != nil {
		return err
	}
	if len(list) == 0 {
		return vm.trap("E004", "col.min: empty list")
	}
	min := list[0]
	for _, v := range list[1:] {
		if valLess(v, min) {
			min = v
		}
	}
	vm.push(min)
	return nil
}

func (vm *VM) builtinColMax() error {
	list, err := vm.popList()
	if err != nil {
		return err
	}
	if len(list) == 0 {
		return vm.trap("E004", "col.max: empty list")
	}
	max := list[0]
	for _, v := range list[1:] {
		if valLess(max, v) {
			max = v
		}
	}
	vm.push(max)
	return nil
}

func (vm *VM) builtinColGroup() error {
	fn, err := vm.pop()
	if err != nil {
		return err
	}
	list, err := vm.popList()
	if err != nil {
		return err
	}
	type group struct {
		key  Value
		vals []Value
	}
	var groups []group
	for _, v := range list {
		key, callErr := vm.callBuiltinFn(fn, []Value{v})
		if callErr != nil {
			return callErr
		}
		found := false
		for i := range groups {
			if ValuesEqual(groups[i].key, key) {
				groups[i].vals = append(groups[i].vals, v)
				found = true
				break
			}
		}
		if !found {
			groups = append(groups, group{key: key, vals: []Value{v}})
		}
	}
	result := make([]Value, len(groups))
	for i, g := range groups {
		result[i] = ValTuple([]Value{g.key, ValList(g.vals)})
	}
	vm.push(ValList(result))
	return nil
}

func (vm *VM) builtinColScan() error {
	fn, err := vm.pop()
	if err != nil {
		return err
	}
	acc, err := vm.pop()
	if err != nil {
		return err
	}
	list, err := vm.popList()
	if err != nil {
		return err
	}
	result := make([]Value, len(list))
	for i, v := range list {
		res, callErr := vm.callBuiltinFn(fn, []Value{acc, v})
		if callErr != nil {
			return callErr
		}
		acc = res
		result[i] = acc
	}
	vm.push(ValList(result))
	return nil
}

// valLess compares two values for ordering.
func valLess(a, b Value) bool {
	if a.Tag == TagSTR && b.Tag == TagSTR {
		return a.StrVal < b.StrVal
	}
	av, _ := NumericValue(a)
	bv, _ := NumericValue(b)
	return av < bv
}

// valShow returns a display string for a value (used by iter.join etc).
func valShow(v Value) string {
	switch v.Tag {
	case TagINT:
		return fmt.Sprintf("%d", v.IntVal)
	case TagRAT:
		return fmt.Sprintf("%g", v.RatVal)
	case TagBOOL:
		if v.BoolVal {
			return "true"
		}
		return "false"
	case TagSTR:
		return v.StrVal
	case TagUNIT:
		return "()"
	default:
		return fmt.Sprintf("%v", v)
	}
}
