package vm

import (
	"fmt"
	"sort"
	"strings"
)

// ── Typeclass builtins (show, eq, clone, cmp for compound types) ──

func (vm *VM) builtinCmp() error {
	b, _ := vm.pop()
	a, _ := vm.pop()
	ltTag, err := vm.findVariantTag("Lt")
	if err != nil {
		return err
	}
	eqTag, err := vm.findVariantTag("Eq_")
	if err != nil {
		return err
	}
	gtTag, err := vm.findVariantTag("Gt")
	if err != nil {
		return err
	}
	var av, bv interface{}
	if a.Tag == TagSTR {
		av = a.StrVal
		bv = b.StrVal
	} else {
		av2, _ := NumericValue(a)
		bv2, _ := NumericValue(b)
		av = av2
		bv = bv2
	}
	switch {
	case fmt.Sprint(av) < fmt.Sprint(bv):
		vm.push(ValUnion(ltTag, nil))
	case fmt.Sprint(av) > fmt.Sprint(bv):
		vm.push(ValUnion(gtTag, nil))
	default:
		vm.push(ValUnion(eqTag, nil))
	}
	return nil
}

func (vm *VM) builtinShowRecord() error {
	rec, _ := vm.pop()
	if rec.Tag != TagHEAP || rec.Heap.Kind != KindRecord {
		return vm.trap("E002", "show$Record: expected record")
	}
	keys := make([]string, 0, len(rec.Heap.Fields))
	for k := range rec.Heap.Fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		v := rec.Heap.Fields[k]
		shown, err := vm.dispatchMethodSync("show", []Value{v})
		if err != nil {
			return err
		}
		if shown.Tag != TagSTR {
			return vm.trap("E002", "show$Record: show did not return Str")
		}
		parts[i] = k + ": " + shown.StrVal
	}
	vm.push(ValStr("{" + strings.Join(parts, ", ") + "}"))
	return nil
}

func (vm *VM) builtinEqRecord() error {
	b, _ := vm.pop()
	a, _ := vm.pop()
	if a.Tag != TagHEAP || a.Heap.Kind != KindRecord || b.Tag != TagHEAP || b.Heap.Kind != KindRecord {
		vm.push(ValBool(false))
		return nil
	}
	if len(a.Heap.Fields) != len(b.Heap.Fields) {
		vm.push(ValBool(false))
		return nil
	}
	for k, av := range a.Heap.Fields {
		bv, ok := b.Heap.Fields[k]
		if !ok {
			vm.push(ValBool(false))
			return nil
		}
		result, err := vm.dispatchMethodSync("eq", []Value{av, bv})
		if err != nil {
			return err
		}
		if result.Tag != TagBOOL || !result.BoolVal {
			vm.push(ValBool(false))
			return nil
		}
	}
	vm.push(ValBool(true))
	return nil
}

func (vm *VM) builtinCloneRecord() error {
	rec, _ := vm.pop()
	if rec.Tag != TagHEAP || rec.Heap.Kind != KindRecord {
		return vm.trap("E002", "clone$Record: expected record")
	}
	newFields := make(map[string]Value, len(rec.Heap.Fields))
	for k, v := range rec.Heap.Fields {
		cloned, err := vm.dispatchMethodSync("clone", []Value{v})
		if err != nil {
			return err
		}
		newFields[k] = cloned
	}
	vm.push(ValRecord(newFields, rec.Heap.FieldOrder))
	return nil
}

func (vm *VM) builtinCmpRecord() error {
	bRec, _ := vm.pop()
	aRec, _ := vm.pop()
	if aRec.Tag != TagHEAP || aRec.Heap.Kind != KindRecord || bRec.Tag != TagHEAP || bRec.Heap.Kind != KindRecord {
		return vm.trap("E002", "cmp$Record: expected records")
	}
	ltTag, _ := vm.findVariantTag("Lt")
	eqTag, _ := vm.findVariantTag("Eq_")
	gtTag, _ := vm.findVariantTag("Gt")

	aKeys := make([]string, 0, len(aRec.Heap.Fields))
	for k := range aRec.Heap.Fields {
		aKeys = append(aKeys, k)
	}
	sort.Strings(aKeys)
	bKeys := make([]string, 0, len(bRec.Heap.Fields))
	for k := range bRec.Heap.Fields {
		bKeys = append(bKeys, k)
	}
	sort.Strings(bKeys)

	minLen := len(aKeys)
	if len(bKeys) < minLen {
		minLen = len(bKeys)
	}
	for i := 0; i < minLen; i++ {
		if aKeys[i] < bKeys[i] {
			vm.push(ValUnion(ltTag, nil))
			return nil
		}
		if aKeys[i] > bKeys[i] {
			vm.push(ValUnion(gtTag, nil))
			return nil
		}
	}
	if len(aKeys) < len(bKeys) {
		vm.push(ValUnion(ltTag, nil))
		return nil
	}
	if len(aKeys) > len(bKeys) {
		vm.push(ValUnion(gtTag, nil))
		return nil
	}
	for _, k := range aKeys {
		r, err := vm.dispatchMethodSync("cmp", []Value{aRec.Heap.Fields[k], bRec.Heap.Fields[k]})
		if err != nil {
			return err
		}
		if r.Tag == TagHEAP && r.Heap.Kind == KindUnion && r.Heap.VariantTag != eqTag {
			vm.push(r)
			return nil
		}
	}
	vm.push(ValUnion(eqTag, nil))
	return nil
}

func (vm *VM) builtinShowList() error {
	lst, _ := vm.pop()
	if lst.Tag != TagHEAP || lst.Heap.Kind != KindList {
		return vm.trap("E002", "show$List: expected list")
	}
	parts := make([]string, len(lst.Heap.Items))
	for i, item := range lst.Heap.Items {
		shown, err := vm.dispatchMethodSync("show", []Value{item})
		if err != nil {
			return err
		}
		if shown.Tag != TagSTR {
			return vm.trap("E002", "show$List: show did not return Str")
		}
		parts[i] = shown.StrVal
	}
	vm.push(ValStr("[" + strings.Join(parts, ", ") + "]"))
	return nil
}

func (vm *VM) builtinEqList() error {
	b, _ := vm.pop()
	a, _ := vm.pop()
	if a.Tag != TagHEAP || a.Heap.Kind != KindList || b.Tag != TagHEAP || b.Heap.Kind != KindList {
		vm.push(ValBool(false))
		return nil
	}
	if len(a.Heap.Items) != len(b.Heap.Items) {
		vm.push(ValBool(false))
		return nil
	}
	for i := range a.Heap.Items {
		r, err := vm.dispatchMethodSync("eq", []Value{a.Heap.Items[i], b.Heap.Items[i]})
		if err != nil {
			return err
		}
		if r.Tag != TagBOOL || !r.BoolVal {
			vm.push(ValBool(false))
			return nil
		}
	}
	vm.push(ValBool(true))
	return nil
}

func (vm *VM) builtinCloneList() error {
	lst, _ := vm.pop()
	if lst.Tag != TagHEAP || lst.Heap.Kind != KindList {
		return vm.trap("E002", "clone$List: expected list")
	}
	cloned := make([]Value, len(lst.Heap.Items))
	for i, item := range lst.Heap.Items {
		c, err := vm.dispatchMethodSync("clone", []Value{item})
		if err != nil {
			return err
		}
		cloned[i] = c
	}
	vm.push(ValList(cloned))
	return nil
}

func (vm *VM) builtinShowTuple() error {
	tup, _ := vm.pop()
	if tup.Tag != TagHEAP || tup.Heap.Kind != KindTuple {
		return vm.trap("E002", "show$Tuple: expected tuple")
	}
	parts := make([]string, len(tup.Heap.Items))
	for i, item := range tup.Heap.Items {
		shown, err := vm.dispatchMethodSync("show", []Value{item})
		if err != nil {
			return err
		}
		if shown.Tag != TagSTR {
			return vm.trap("E002", "show$Tuple: show did not return Str")
		}
		parts[i] = shown.StrVal
	}
	vm.push(ValStr("(" + strings.Join(parts, ", ") + ")"))
	return nil
}

func (vm *VM) builtinEqTuple() error {
	b, _ := vm.pop()
	a, _ := vm.pop()
	if a.Tag != TagHEAP || a.Heap.Kind != KindTuple || b.Tag != TagHEAP || b.Heap.Kind != KindTuple {
		vm.push(ValBool(false))
		return nil
	}
	if len(a.Heap.Items) != len(b.Heap.Items) {
		vm.push(ValBool(false))
		return nil
	}
	for i := range a.Heap.Items {
		r, err := vm.dispatchMethodSync("eq", []Value{a.Heap.Items[i], b.Heap.Items[i]})
		if err != nil {
			return err
		}
		if r.Tag != TagBOOL || !r.BoolVal {
			vm.push(ValBool(false))
			return nil
		}
	}
	vm.push(ValBool(true))
	return nil
}

func (vm *VM) builtinCloneTuple() error {
	tup, _ := vm.pop()
	if tup.Tag != TagHEAP || tup.Heap.Kind != KindTuple {
		return vm.trap("E002", "clone$Tuple: expected tuple")
	}
	cloned := make([]Value, len(tup.Heap.Items))
	for i, item := range tup.Heap.Items {
		c, err := vm.dispatchMethodSync("clone", []Value{item})
		if err != nil {
			return err
		}
		cloned[i] = c
	}
	vm.push(ValTuple(cloned))
	return nil
}

func (vm *VM) builtinCmpList() error {
	b, _ := vm.pop()
	a, _ := vm.pop()
	if a.Tag != TagHEAP || a.Heap.Kind != KindList || b.Tag != TagHEAP || b.Heap.Kind != KindList {
		return vm.trap("E002", "cmp$List: expected lists")
	}
	ltTag, _ := vm.findVariantTag("Lt")
	eqTag, _ := vm.findVariantTag("Eq_")
	gtTag, _ := vm.findVariantTag("Gt")
	minLen := len(a.Heap.Items)
	if len(b.Heap.Items) < minLen {
		minLen = len(b.Heap.Items)
	}
	for i := 0; i < minLen; i++ {
		r, err := vm.dispatchMethodSync("cmp", []Value{a.Heap.Items[i], b.Heap.Items[i]})
		if err != nil {
			return err
		}
		if r.Tag == TagHEAP && r.Heap.Kind == KindUnion && r.Heap.VariantTag != eqTag {
			vm.push(r)
			return nil
		}
	}
	if len(a.Heap.Items) < len(b.Heap.Items) {
		vm.push(ValUnion(ltTag, nil))
	} else if len(a.Heap.Items) > len(b.Heap.Items) {
		vm.push(ValUnion(gtTag, nil))
	} else {
		vm.push(ValUnion(eqTag, nil))
	}
	return nil
}

func (vm *VM) builtinCmpTuple() error {
	b, _ := vm.pop()
	a, _ := vm.pop()
	if a.Tag != TagHEAP || a.Heap.Kind != KindTuple || b.Tag != TagHEAP || b.Heap.Kind != KindTuple {
		return vm.trap("E002", "cmp$Tuple: expected tuples")
	}
	ltTag, _ := vm.findVariantTag("Lt")
	eqTag, _ := vm.findVariantTag("Eq_")
	gtTag, _ := vm.findVariantTag("Gt")
	minLen := len(a.Heap.Items)
	if len(b.Heap.Items) < minLen {
		minLen = len(b.Heap.Items)
	}
	for i := 0; i < minLen; i++ {
		r, err := vm.dispatchMethodSync("cmp", []Value{a.Heap.Items[i], b.Heap.Items[i]})
		if err != nil {
			return err
		}
		if r.Tag == TagHEAP && r.Heap.Kind == KindUnion && r.Heap.VariantTag != eqTag {
			vm.push(r)
			return nil
		}
	}
	if len(a.Heap.Items) < len(b.Heap.Items) {
		vm.push(ValUnion(ltTag, nil))
	} else if len(a.Heap.Items) > len(b.Heap.Items) {
		vm.push(ValUnion(gtTag, nil))
	} else {
		vm.push(ValUnion(eqTag, nil))
	}
	return nil
}

func (vm *VM) builtinCloneRef() error {
	v, _ := vm.pop()
	if v.Tag != TagHEAP || v.Heap.Kind != KindRef {
		return vm.trap("E002", "clone$Ref: expected Ref")
	}
	v.Heap.Ref.HandleCount++
	vm.push(ValRef(v.Heap.Ref))
	return nil
}

func (vm *VM) builtinCloneTVar() error {
	v, _ := vm.pop()
	if v.Tag != TagHEAP || v.Heap.Kind != KindTVar {
		return vm.trap("E002", "clone$TVar: expected TVar")
	}
	v.Heap.TVar.HandleCount++
	vm.push(ValTVarVal(v.Heap.TVar))
	return nil
}
