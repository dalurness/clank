package vm

import (
	"encoding/json"
	"fmt"
	"sort"
)

// ── JSON (json.*) ──

func (vm *VM) builtinJsonEnc() error {
	v, err := vm.pop()
	if err != nil {
		return err
	}
	goVal := valueToGo(v)
	data, jsonErr := json.Marshal(goVal)
	if jsonErr != nil {
		return vm.trap("E901", fmt.Sprintf("json.enc: %v", jsonErr))
	}
	vm.push(ValStr(string(data)))
	return nil
}

func (vm *VM) builtinJsonDec() error {
	s, err := vm.popStr()
	if err != nil {
		return err
	}
	var raw interface{}
	if jsonErr := json.Unmarshal([]byte(s), &raw); jsonErr != nil {
		return vm.trap("E901", fmt.Sprintf("json.dec: %v", jsonErr))
	}
	vm.push(goToValue(raw))
	return nil
}

func (vm *VM) builtinJsonGet() error {
	key, err := vm.popStr()
	if err != nil {
		return err
	}
	obj, err := vm.pop()
	if err != nil {
		return err
	}
	if obj.Tag == TagHEAP && obj.Heap.Kind == KindRecord {
		if val, ok := obj.Heap.Fields[key]; ok {
			someTag, _ := vm.findVariantTag("Some")
			vm.push(ValUnion(someTag, []Value{val}))
		} else {
			noneTag, _ := vm.findVariantTag("None")
			vm.push(ValUnion(noneTag, nil))
		}
		return nil
	}
	noneTag, _ := vm.findVariantTag("None")
	vm.push(ValUnion(noneTag, nil))
	return nil
}

func (vm *VM) builtinJsonSet() error {
	val, err := vm.pop()
	if err != nil {
		return err
	}
	key, err := vm.popStr()
	if err != nil {
		return err
	}
	obj, err := vm.pop()
	if err != nil {
		return err
	}
	if obj.Tag != TagHEAP || obj.Heap.Kind != KindRecord {
		return vm.trap("E901", "json.set: expected record")
	}
	newFields := make(map[string]Value)
	newOrder := make([]string, len(obj.Heap.FieldOrder))
	copy(newOrder, obj.Heap.FieldOrder)
	for k, v := range obj.Heap.Fields {
		newFields[k] = v
	}
	if _, exists := newFields[key]; !exists {
		newOrder = append(newOrder, key)
	}
	newFields[key] = val
	vm.push(ValRecord(newFields, newOrder))
	return nil
}

func (vm *VM) builtinJsonKeys() error {
	obj, err := vm.pop()
	if err != nil {
		return err
	}
	if obj.Tag != TagHEAP || obj.Heap.Kind != KindRecord {
		return vm.trap("E901", "json.keys: expected record")
	}
	order := obj.Heap.FieldOrder
	if len(order) == 0 {
		for k := range obj.Heap.Fields {
			order = append(order, k)
		}
		sort.Strings(order)
	}
	items := make([]Value, len(order))
	for i, k := range order {
		items[i] = ValStr(k)
	}
	vm.push(ValList(items))
	return nil
}

func (vm *VM) builtinJsonMerge() error {
	b, err := vm.pop()
	if err != nil {
		return err
	}
	a, err := vm.pop()
	if err != nil {
		return err
	}
	if a.Tag != TagHEAP || a.Heap.Kind != KindRecord || b.Tag != TagHEAP || b.Heap.Kind != KindRecord {
		return vm.trap("E901", "json.merge: expected two records")
	}
	newFields := make(map[string]Value)
	var newOrder []string
	for _, k := range a.Heap.FieldOrder {
		newFields[k] = a.Heap.Fields[k]
		newOrder = append(newOrder, k)
	}
	for _, k := range b.Heap.FieldOrder {
		if _, exists := newFields[k]; !exists {
			newOrder = append(newOrder, k)
		}
		newFields[k] = b.Heap.Fields[k]
	}
	vm.push(ValRecord(newFields, newOrder))
	return nil
}

// ── Helpers for JSON conversion ──

func valueToGo(v Value) interface{} {
	switch v.Tag {
	case TagINT:
		return v.IntVal
	case TagRAT:
		return v.RatVal
	case TagBOOL:
		return v.BoolVal
	case TagSTR:
		return v.StrVal
	case TagUNIT:
		return nil
	case TagHEAP:
		switch v.Heap.Kind {
		case KindList:
			items := make([]interface{}, len(v.Heap.Items))
			for i, el := range v.Heap.Items {
				items[i] = valueToGo(el)
			}
			return items
		case KindTuple:
			items := make([]interface{}, len(v.Heap.Items))
			for i, el := range v.Heap.Items {
				items[i] = valueToGo(el)
			}
			return items
		case KindRecord:
			obj := make(map[string]interface{})
			for k, val := range v.Heap.Fields {
				obj[k] = valueToGo(val)
			}
			return obj
		case KindUnion:
			if len(activeVariantNames) > v.Heap.VariantTag {
				name := activeVariantNames[v.Heap.VariantTag]
				if name == "None" && len(v.Heap.UFields) == 0 {
					return nil
				}
				if name == "Some" && len(v.Heap.UFields) == 1 {
					return valueToGo(v.Heap.UFields[0])
				}
			}
			return ValueToString(v)
		}
	}
	return nil
}

func goToValue(v interface{}) Value {
	if v == nil {
		return ValUnit()
	}
	switch val := v.(type) {
	case float64:
		if val == float64(int(val)) && val >= -9.2e18 && val <= 9.2e18 {
			return ValInt(int(val))
		}
		return ValRat(val)
	case bool:
		return ValBool(val)
	case string:
		return ValStr(val)
	case []interface{}:
		items := make([]Value, len(val))
		for i, el := range val {
			items[i] = goToValue(el)
		}
		return ValList(items)
	case map[string]interface{}:
		fields := make(map[string]Value)
		var order []string
		for k, v := range val {
			order = append(order, k)
			fields[k] = goToValue(v)
		}
		sort.Strings(order)
		return ValRecord(fields, order)
	}
	return ValStr(fmt.Sprintf("%v", v))
}
