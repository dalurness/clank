package vm

import "math"

// ── Math (math.*) ──

func (vm *VM) builtinMathAbs() error {
	v, err := vm.pop()
	if err != nil {
		return err
	}
	switch v.Tag {
	case TagINT:
		if v.IntVal < 0 {
			vm.push(ValInt(-v.IntVal))
		} else {
			vm.push(v)
		}
	case TagRAT:
		vm.push(ValRat(math.Abs(v.RatVal)))
	default:
		return vm.trap("E002", "math.abs: expected numeric type")
	}
	return nil
}

func (vm *VM) builtinMathMin() error {
	b, err := vm.pop()
	if err != nil {
		return err
	}
	a, err := vm.pop()
	if err != nil {
		return err
	}
	av, aok := NumericValue(a)
	bv, bok := NumericValue(b)
	if !aok || !bok {
		return vm.trap("E002", "math.min: expected numeric type")
	}
	if av <= bv {
		vm.push(a)
	} else {
		vm.push(b)
	}
	return nil
}

func (vm *VM) builtinMathMax() error {
	b, err := vm.pop()
	if err != nil {
		return err
	}
	a, err := vm.pop()
	if err != nil {
		return err
	}
	av, aok := NumericValue(a)
	bv, bok := NumericValue(b)
	if !aok || !bok {
		return vm.trap("E002", "math.max: expected numeric type")
	}
	if av >= bv {
		vm.push(a)
	} else {
		vm.push(b)
	}
	return nil
}

func (vm *VM) builtinMathFloor() error {
	v, err := vm.pop()
	if err != nil {
		return err
	}
	fv, ok := NumericValue(v)
	if !ok {
		return vm.trap("E002", "math.floor: expected numeric type")
	}
	vm.push(ValInt(int(math.Floor(fv))))
	return nil
}

func (vm *VM) builtinMathCeil() error {
	v, err := vm.pop()
	if err != nil {
		return err
	}
	fv, ok := NumericValue(v)
	if !ok {
		return vm.trap("E002", "math.ceil: expected numeric type")
	}
	vm.push(ValInt(int(math.Ceil(fv))))
	return nil
}

func (vm *VM) builtinMathSqrt() error {
	v, err := vm.pop()
	if err != nil {
		return err
	}
	fv, ok := NumericValue(v)
	if !ok {
		return vm.trap("E002", "math.sqrt: expected numeric type")
	}
	if fv < 0 {
		return vm.trap("E003", "math.sqrt: negative argument")
	}
	vm.push(ValRat(math.Sqrt(fv)))
	return nil
}
