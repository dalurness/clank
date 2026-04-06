package vm

import (
	"fmt"
	"os"
	"strings"
)

// ── Environment (env.*) ──

func (vm *VM) builtinEnvGet() error {
	key, err := vm.popStr()
	if err != nil {
		return err
	}
	val, ok := os.LookupEnv(key)
	if ok {
		someTag, _ := vm.findVariantTag("Some")
		vm.push(ValUnion(someTag, []Value{ValStr(val)}))
	} else {
		noneTag, _ := vm.findVariantTag("None")
		vm.push(ValUnion(noneTag, nil))
	}
	return nil
}

func (vm *VM) builtinEnvSet() error {
	val, err := vm.popStr()
	if err != nil {
		return err
	}
	key, err := vm.popStr()
	if err != nil {
		return err
	}
	if setErr := os.Setenv(key, val); setErr != nil {
		return vm.trap("E902", fmt.Sprintf("env.set: %v", setErr))
	}
	vm.push(ValUnit())
	return nil
}

func (vm *VM) builtinEnvHas() error {
	key, err := vm.popStr()
	if err != nil {
		return err
	}
	_, ok := os.LookupEnv(key)
	vm.push(ValBool(ok))
	return nil
}

func (vm *VM) builtinEnvAll() error {
	environ := os.Environ()
	items := make([]Value, 0, len(environ))
	for _, e := range environ {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) == 2 {
			items = append(items, ValTuple([]Value{ValStr(parts[0]), ValStr(parts[1])}))
		}
	}
	vm.push(ValList(items))
	return nil
}
