package vm

import (
	"fmt"
	"os"
)

// ── Filesystem (fs.*) ──

func (vm *VM) builtinFsRead() error {
	path, err := vm.popStr()
	if err != nil {
		return err
	}
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		return vm.trap("E900", fmt.Sprintf("fs.read: %v", readErr))
	}
	vm.push(ValStr(string(data)))
	return nil
}

func (vm *VM) builtinFsWrite() error {
	content, err := vm.popStr()
	if err != nil {
		return err
	}
	path, err := vm.popStr()
	if err != nil {
		return err
	}
	if writeErr := os.WriteFile(path, []byte(content), 0644); writeErr != nil {
		return vm.trap("E900", fmt.Sprintf("fs.write: %v", writeErr))
	}
	vm.push(ValUnit())
	return nil
}

func (vm *VM) builtinFsExists() error {
	path, err := vm.popStr()
	if err != nil {
		return err
	}
	_, statErr := os.Stat(path)
	vm.push(ValBool(statErr == nil))
	return nil
}

func (vm *VM) builtinFsLs() error {
	path, err := vm.popStr()
	if err != nil {
		return err
	}
	entries, readErr := os.ReadDir(path)
	if readErr != nil {
		return vm.trap("E900", fmt.Sprintf("fs.ls: %v", readErr))
	}
	items := make([]Value, len(entries))
	for i, e := range entries {
		items[i] = ValStr(e.Name())
	}
	vm.push(ValList(items))
	return nil
}

func (vm *VM) builtinFsMkdir() error {
	path, err := vm.popStr()
	if err != nil {
		return err
	}
	if mkErr := os.MkdirAll(path, 0755); mkErr != nil {
		return vm.trap("E900", fmt.Sprintf("fs.mkdir: %v", mkErr))
	}
	vm.push(ValUnit())
	return nil
}

func (vm *VM) builtinFsRm() error {
	path, err := vm.popStr()
	if err != nil {
		return err
	}
	if rmErr := os.RemoveAll(path); rmErr != nil {
		return vm.trap("E900", fmt.Sprintf("fs.rm: %v", rmErr))
	}
	vm.push(ValUnit())
	return nil
}
