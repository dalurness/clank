package vm

import (
	"fmt"
	"os"
	"strings"
)

// ── std.cli — Command-line argument parsing ──

func (vm *VM) builtinCliArgs() error {
	args := os.Args[1:]
	root := vm.root()
	if root.testArgs != nil {
		args = root.testArgs
	}
	items := make([]Value, len(args))
	for i, a := range args {
		items[i] = ValStr(a)
	}
	vm.push(ValList(items))
	return nil
}

func (vm *VM) builtinCliParse() error {
	optsList, err := vm.popList()
	if err != nil {
		return err
	}
	args := os.Args[1:]
	root := vm.root()
	if root.testArgs != nil {
		args = root.testArgs
	}

	// Extract option descriptors
	type optDesc struct {
		name     string
		short    string
		required bool
		defVal   string
		hasDef   bool
	}
	var descs []optDesc
	for _, ov := range optsList {
		if ov.Tag != TagHEAP || ov.Heap.Kind != KindRecord {
			continue
		}
		f := ov.Heap.Fields
		d := optDesc{}
		if n, ok := f["name"]; ok && n.Tag == TagSTR {
			d.name = n.StrVal
		}
		if s, ok := f["short"]; ok && s.Tag == TagSTR {
			d.short = s.StrVal
		}
		if r, ok := f["required"]; ok && r.Tag == TagBOOL {
			d.required = r.BoolVal
		}
		if dv, ok := f["default"]; ok {
			if dv.Tag == TagHEAP && dv.Heap.Kind == KindUnion {
				if dv.Heap.VariantTag < len(vm.variantNames) && vm.variantNames[dv.Heap.VariantTag] == "Some" && len(dv.Heap.UFields) > 0 {
					d.hasDef = true
					d.defVal = dv.Heap.UFields[0].StrVal
				}
			}
		}
		descs = append(descs, d)
	}

	// Parse arguments
	opts := make(map[string]Value)
	optsOrder := []string{}
	flags := make(map[string]Value)
	flagsOrder := []string{}
	var positional []Value
	cmd := ""
	if len(args) > 0 {
		cmd = args[0]
	}

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "--") {
			key := arg[2:]
			if idx := strings.Index(key, "="); idx >= 0 {
				name := key[:idx]
				val := key[idx+1:]
				opts[name] = ValStr(val)
				optsOrder = append(optsOrder, name)
			} else if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				// Check if this is a known option (has value) or a flag
				isOpt := false
				for _, d := range descs {
					if d.name == key {
						isOpt = true
						break
					}
				}
				if isOpt {
					i++
					opts[key] = ValStr(args[i])
					optsOrder = append(optsOrder, key)
				} else {
					flags[key] = ValBool(true)
					flagsOrder = append(flagsOrder, key)
				}
			} else {
				flags[key] = ValBool(true)
				flagsOrder = append(flagsOrder, key)
			}
		} else if strings.HasPrefix(arg, "-") && len(arg) == 2 {
			short := string(arg[1])
			// Find matching option by short name
			found := false
			for _, d := range descs {
				if d.short == short {
					if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
						i++
						opts[d.name] = ValStr(args[i])
						optsOrder = append(optsOrder, d.name)
					} else {
						flags[d.name] = ValBool(true)
						flagsOrder = append(flagsOrder, d.name)
					}
					found = true
					break
				}
			}
			if !found {
				flags[short] = ValBool(true)
				flagsOrder = append(flagsOrder, short)
			}
		} else {
			positional = append(positional, ValStr(arg))
		}
	}

	// Apply defaults for missing options
	for _, d := range descs {
		if _, ok := opts[d.name]; !ok && d.hasDef {
			opts[d.name] = ValStr(d.defVal)
			optsOrder = append(optsOrder, d.name)
		}
	}

	// Check required options
	for _, d := range descs {
		if d.required {
			if _, ok := opts[d.name]; !ok {
				return vm.trap("E900", fmt.Sprintf("cli.parse: required option --%s missing", d.name))
			}
		}
	}

	result := ValRecord(map[string]Value{
		"cmd":   ValStr(cmd),
		"args":  ValList(positional),
		"opts":  ValRecord(opts, optsOrder),
		"flags": ValRecord(flags, flagsOrder),
	}, []string{"cmd", "args", "opts", "flags"})
	vm.push(result)
	return nil
}

func (vm *VM) builtinCliOpt() error {
	desc, err := vm.popStr()
	if err != nil {
		return err
	}
	short, err := vm.popStr()
	if err != nil {
		return err
	}
	name, err := vm.popStr()
	if err != nil {
		return err
	}
	noneTag, _ := vm.findVariantTag("None")
	result := ValRecord(map[string]Value{
		"name":     ValStr(name),
		"short":    ValStr(short),
		"desc":     ValStr(desc),
		"required": ValBool(false),
		"default":  ValUnion(noneTag, nil),
	}, []string{"name", "short", "desc", "required", "default"})
	vm.push(result)
	return nil
}

func (vm *VM) builtinCliReq() error {
	rec, _ := vm.pop()
	if rec.Tag != TagHEAP || rec.Heap.Kind != KindRecord {
		return vm.trap("E002", "cli.req: expected option record")
	}
	newFields := make(map[string]Value)
	for k, v := range rec.Heap.Fields {
		newFields[k] = v
	}
	newFields["required"] = ValBool(true)
	order := make([]string, len(rec.Heap.FieldOrder))
	copy(order, rec.Heap.FieldOrder)
	vm.push(ValRecord(newFields, order))
	return nil
}

func (vm *VM) builtinCliDef() error {
	defStr, err := vm.popStr()
	if err != nil {
		return err
	}
	rec, _ := vm.pop()
	if rec.Tag != TagHEAP || rec.Heap.Kind != KindRecord {
		return vm.trap("E002", "cli.def: expected option record")
	}
	someTag, _ := vm.findVariantTag("Some")
	newFields := make(map[string]Value)
	for k, v := range rec.Heap.Fields {
		newFields[k] = v
	}
	newFields["default"] = ValUnion(someTag, []Value{ValStr(defStr)})
	order := make([]string, len(rec.Heap.FieldOrder))
	copy(order, rec.Heap.FieldOrder)
	vm.push(ValRecord(newFields, order))
	return nil
}

func (vm *VM) builtinCliGet() error {
	name, err := vm.popStr()
	if err != nil {
		return err
	}
	rec, _ := vm.pop()
	if rec.Tag != TagHEAP || rec.Heap.Kind != KindRecord {
		return vm.trap("E002", "cli.get: expected CliArgs record")
	}
	someTag, _ := vm.findVariantTag("Some")
	noneTag, _ := vm.findVariantTag("None")
	optsVal, ok := rec.Heap.Fields["opts"]
	if ok && optsVal.Tag == TagHEAP && optsVal.Heap.Kind == KindRecord {
		if v, found := optsVal.Heap.Fields[name]; found {
			vm.push(ValUnion(someTag, []Value{v}))
			return nil
		}
	}
	vm.push(ValUnion(noneTag, nil))
	return nil
}

func (vm *VM) builtinCliFlag() error {
	name, err := vm.popStr()
	if err != nil {
		return err
	}
	rec, _ := vm.pop()
	if rec.Tag != TagHEAP || rec.Heap.Kind != KindRecord {
		return vm.trap("E002", "cli.flag: expected CliArgs record")
	}
	flagsVal, ok := rec.Heap.Fields["flags"]
	if ok && flagsVal.Tag == TagHEAP && flagsVal.Heap.Kind == KindRecord {
		if _, found := flagsVal.Heap.Fields[name]; found {
			vm.push(ValBool(true))
			return nil
		}
	}
	vm.push(ValBool(false))
	return nil
}

func (vm *VM) builtinCliPos() error {
	idxVal, _ := vm.pop()
	rec, _ := vm.pop()
	if rec.Tag != TagHEAP || rec.Heap.Kind != KindRecord {
		return vm.trap("E002", "cli.pos: expected CliArgs record")
	}
	idx := numVal(idxVal)
	someTag, _ := vm.findVariantTag("Some")
	noneTag, _ := vm.findVariantTag("None")
	argsVal, ok := rec.Heap.Fields["args"]
	if ok && argsVal.Tag == TagHEAP && argsVal.Heap.Kind == KindList {
		if idx >= 0 && idx < len(argsVal.Heap.Items) {
			vm.push(ValUnion(someTag, []Value{argsVal.Heap.Items[idx]}))
			return nil
		}
	}
	vm.push(ValUnion(noneTag, nil))
	return nil
}
