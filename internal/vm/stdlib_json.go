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
		return vm.raiseOrTrap("E901", fmt.Sprintf("json.dec: %v", jsonErr))
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

// ── Typed decoding (json.as / json.or) ──

// json.as(raw, shape) -> shape's type
// Validates raw against a template value that doubles as the schema:
// json.as(json.dec(body), {name: "", age: 0, tags: [""]}) yields a record
// the checker knows as {name: Str, age: Int, tags: [Str]}. Template
// fields of Some(exemplar) are optional (missing -> None). Extra JSON
// fields are dropped. Mismatches raise E901 with a path.
func (vm *VM) builtinJsonAs() error {
	tmpl, err := vm.pop()
	if err != nil {
		return err
	}
	raw, err := vm.pop()
	if err != nil {
		return err
	}
	out, msg := vm.validateJSON(raw, tmpl, "", false)
	if msg != "" {
		return vm.raiseOrTrap("E901", "json.as: "+msg)
	}
	vm.push(out)
	return nil
}

// json.or(raw, defaults) -> defaults' type
// Like json.as but lenient: a missing or mismatched field takes the
// template's value instead of raising. Never fails.
func (vm *VM) builtinJsonOr() error {
	tmpl, err := vm.pop()
	if err != nil {
		return err
	}
	raw, err := vm.pop()
	if err != nil {
		return err
	}
	out, _ := vm.validateJSON(raw, tmpl, "", true)
	vm.push(out)
	return nil
}

func jsonTypeName(v Value) string {
	switch v.Tag {
	case TagINT:
		return "Int"
	case TagRAT:
		return "Rat"
	case TagBOOL:
		return "Bool"
	case TagSTR:
		return "Str"
	case TagUNIT:
		return "null"
	case TagHEAP:
		switch v.Heap.Kind {
		case KindRecord:
			return "record"
		case KindList:
			return "list"
		case KindTuple:
			return "tuple"
		case KindUnion:
			if len(activeVariantNames) > v.Heap.VariantTag {
				return activeVariantNames[v.Heap.VariantTag]
			}
			return "union"
		}
		return string(v.Heap.Kind)
	}
	return "value"
}

func joinPath(path, field string) string {
	if path == "" {
		return field
	}
	return path + "." + field
}

// validateJSON checks value against tmpl. Returns (result, "") on success;
// on mismatch, lenient mode substitutes tmpl and strict mode returns a
// path-qualified error message.
func (vm *VM) validateJSON(value, tmpl Value, path string, lenient bool) (Value, string) {
	mismatch := func(want string) (Value, string) {
		if lenient {
			return tmpl, ""
		}
		at := path
		if at == "" {
			at = "value"
		}
		return tmpl, fmt.Sprintf("%s: expected %s, got %s", at, want, jsonTypeName(value))
	}

	switch tmpl.Tag {
	case TagINT:
		if value.Tag == TagINT {
			return value, ""
		}
		return mismatch("Int")
	case TagRAT:
		if value.Tag == TagRAT {
			return value, ""
		}
		if value.Tag == TagINT {
			return ValRat(float64(value.IntVal)), ""
		}
		return mismatch("Rat")
	case TagBOOL:
		if value.Tag == TagBOOL {
			return value, ""
		}
		return mismatch("Bool")
	case TagSTR:
		if value.Tag == TagSTR {
			return value, ""
		}
		return mismatch("Str")
	case TagUNIT:
		if value.Tag == TagUNIT {
			return value, ""
		}
		return mismatch("null")
	case TagHEAP:
		switch tmpl.Heap.Kind {
		case KindRecord:
			if value.Tag != TagHEAP || value.Heap.Kind != KindRecord {
				return mismatch("record")
			}
			fields := make(map[string]Value, len(tmpl.Heap.FieldOrder))
			order := make([]string, len(tmpl.Heap.FieldOrder))
			copy(order, tmpl.Heap.FieldOrder)
			for _, k := range tmpl.Heap.FieldOrder {
				ft := tmpl.Heap.Fields[k]
				fv, present := value.Heap.Fields[k]
				if !present || (fv.Tag == TagUNIT && ft.Tag != TagUNIT) {
					// Optional field: Some(exemplar) template -> None when absent
					if isSomeTemplate(ft) {
						noneTag, _ := vm.findVariantTag("None")
						fields[k] = ValUnion(noneTag, nil)
						continue
					}
					if lenient {
						fields[k] = ft
						continue
					}
					return tmpl, fmt.Sprintf("%s: missing", joinPath(path, k))
				}
				out, msg := vm.validateJSON(fv, ft, joinPath(path, k), lenient)
				if msg != "" {
					return tmpl, msg
				}
				fields[k] = out
			}
			return ValRecord(fields, order), ""
		case KindList:
			if value.Tag != TagHEAP || value.Heap.Kind != KindList {
				return mismatch("list")
			}
			if len(tmpl.Heap.Items) == 0 {
				return value, ""
			}
			elemTmpl := tmpl.Heap.Items[0]
			items := make([]Value, len(value.Heap.Items))
			for i, el := range value.Heap.Items {
				out, msg := vm.validateJSON(el, elemTmpl, fmt.Sprintf("%s[%d]", path, i), lenient)
				if msg != "" {
					return tmpl, msg
				}
				items[i] = out
			}
			return ValList(items), ""
		case KindUnion:
			// Some(exemplar) marks an optional value: null -> None,
			// anything else validates against the exemplar.
			if isSomeTemplate(tmpl) {
				someTag, _ := vm.findVariantTag("Some")
				noneTag, _ := vm.findVariantTag("None")
				inner := value
				if value.Tag == TagUNIT {
					return ValUnion(noneTag, nil), ""
				}
				if value.Tag == TagHEAP && value.Heap.Kind == KindUnion {
					name := ""
					if len(activeVariantNames) > value.Heap.VariantTag {
						name = activeVariantNames[value.Heap.VariantTag]
					}
					if name == "None" {
						return ValUnion(noneTag, nil), ""
					}
					if name == "Some" && len(value.Heap.UFields) == 1 {
						inner = value.Heap.UFields[0]
					}
				}
				out, msg := vm.validateJSON(inner, tmpl.Heap.UFields[0], path, lenient)
				if msg != "" {
					return tmpl, msg
				}
				return ValUnion(someTag, []Value{out}), ""
			}
			return tmpl, fmt.Sprintf("%s: unsupported template %s (optional fields use Some(exemplar))", joinPath(path, ""), jsonTypeName(tmpl))
		}
		return tmpl, fmt.Sprintf("%s: unsupported template type %s", joinPath(path, ""), jsonTypeName(tmpl))
	}
	return tmpl, fmt.Sprintf("%s: unsupported template type %s", joinPath(path, ""), jsonTypeName(tmpl))
}

func isSomeTemplate(v Value) bool {
	if v.Tag != TagHEAP || v.Heap.Kind != KindUnion || len(v.Heap.UFields) != 1 {
		return false
	}
	return len(activeVariantNames) > v.Heap.VariantTag && activeVariantNames[v.Heap.VariantTag] == "Some"
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
