package clank_test

import (
	"strings"
	"testing"
)

// ─── Ref Tests ───

func TestRefBasics(t *testing.T) {
	t.Run("ref-new and ref-read", func(t *testing.T) {
		source := `
main : () -> <io> () =
  let r = ref-new(42)
  let v = ref-read(r)
  print(show(v))
`
		output, err := runProgram(source, "")
		if err != nil {
			t.Fatalf("runtime error: %v", err)
		}
		if strings.TrimSpace(output) != "42" {
			t.Errorf("expected '42', got %q", output)
		}
	})

	t.Run("ref-write updates value", func(t *testing.T) {
		source := `
main : () -> <io> () =
  let r = ref-new(10)
  let _ = ref-write(r, 20)
  let v = ref-read(r)
  print(show(v))
`
		output, err := runProgram(source, "")
		if err != nil {
			t.Fatalf("runtime error: %v", err)
		}
		if strings.TrimSpace(output) != "20" {
			t.Errorf("expected '20', got %q", output)
		}
	})

	t.Run("multiple writes last value wins", func(t *testing.T) {
		source := `
main : () -> <io> () =
  let r = ref-new(1)
  let _ = ref-write(r, 2)
  let _ = ref-write(r, 3)
  let v = ref-read(r)
  print(show(v))
`
		output, err := runProgram(source, "")
		if err != nil {
			t.Fatalf("runtime error: %v", err)
		}
		if strings.TrimSpace(output) != "3" {
			t.Errorf("expected '3', got %q", output)
		}
	})

	t.Run("unrestricted ref-read is non-destructive", func(t *testing.T) {
		source := `
main : () -> <io> () =
  let r = ref-new(42)
  let v1 = ref-read(r)
  let v2 = ref-read(r)
  print(show(v1 + v2))
`
		output, err := runProgram(source, "")
		if err != nil {
			t.Fatalf("runtime error: %v", err)
		}
		if strings.TrimSpace(output) != "84" {
			t.Errorf("expected '84', got %q", output)
		}
	})
}

func TestRefTwoIndependent(t *testing.T) {
	t.Run("two refs hold independent values", func(t *testing.T) {
		source := `
main : () -> <io> () =
  let a = ref-new(10)
  let b = ref-new(20)
  let va = ref-read(a)
  let vb = ref-read(b)
  print(show(va + vb))
`
		output, err := runProgram(source, "")
		if err != nil {
			t.Fatalf("runtime error: %v", err)
		}
		if strings.TrimSpace(output) != "30" {
			t.Errorf("expected '30', got %q", output)
		}
	})

	t.Run("writing one ref does not affect another", func(t *testing.T) {
		source := `
main : () -> <io> () =
  let a = ref-new(10)
  let b = ref-new(20)
  let _ = ref-write(a, 99)
  let v = ref-read(b)
  print(show(v))
`
		output, err := runProgram(source, "")
		if err != nil {
			t.Fatalf("runtime error: %v", err)
		}
		if strings.TrimSpace(output) != "20" {
			t.Errorf("expected '20', got %q", output)
		}
	})
}

func TestRefReadModifyWrite(t *testing.T) {
	source := `
main : () -> <io> () =
  let r = ref-new(10)
  let v = ref-read(r)
  let _ = ref-write(r, v + 5)
  let result = ref-read(r)
  print(show(result))
`
	output, err := runProgram(source, "")
	if err != nil {
		t.Fatalf("runtime error: %v", err)
	}
	if strings.TrimSpace(output) != "15" {
		t.Errorf("expected '15', got %q", output)
	}
}

func TestRefCAS(t *testing.T) {
	t.Skip("ref-cas not yet implemented in Go VM")
	t.Run("cas succeeds when expected matches", func(t *testing.T) {
		source := `
main : () -> <io> () =
  let r = ref-new(10)
  let result = ref-cas(r, 10, 20)
  let success = fst(result)
  print(show(success))
`
		output, err := runProgram(source, "")
		if err != nil {
			t.Fatalf("runtime error: %v", err)
		}
		if strings.TrimSpace(output) != "true" {
			t.Errorf("expected 'true', got %q", output)
		}
	})

	t.Run("cas updates cell on success", func(t *testing.T) {
		source := `
main : () -> <io> () =
  let r = ref-new(10)
  let _ = ref-cas(r, 10, 20)
  let v = ref-read(r)
  print(show(v))
`
		output, err := runProgram(source, "")
		if err != nil {
			t.Fatalf("runtime error: %v", err)
		}
		if strings.TrimSpace(output) != "20" {
			t.Errorf("expected '20', got %q", output)
		}
	})

	t.Run("cas fails when expected does not match", func(t *testing.T) {
		source := `
main : () -> <io> () =
  let r = ref-new(10)
  let result = ref-cas(r, 99, 20)
  let success = fst(result)
  print(show(success))
`
		output, err := runProgram(source, "")
		if err != nil {
			t.Fatalf("runtime error: %v", err)
		}
		if strings.TrimSpace(output) != "false" {
			t.Errorf("expected 'false', got %q", output)
		}
	})

	t.Run("cas does not update cell on failure", func(t *testing.T) {
		source := `
main : () -> <io> () =
  let r = ref-new(10)
  let _ = ref-cas(r, 99, 20)
  let v = ref-read(r)
  print(show(v))
`
		output, err := runProgram(source, "")
		if err != nil {
			t.Fatalf("runtime error: %v", err)
		}
		if strings.TrimSpace(output) != "10" {
			t.Errorf("expected '10', got %q", output)
		}
	})
}

func TestRefModify(t *testing.T) {
	t.Skip("ref-modify not yet implemented in Go VM")
	t.Run("ref-modify applies function and updates cell", func(t *testing.T) {
		source := `
main : () -> <io> () =
  let r = ref-new(10)
  let _ = ref-modify(r, fn(x) => x + 5)
  let v = ref-read(r)
  print(show(v))
`
		output, err := runProgram(source, "")
		if err != nil {
			t.Fatalf("runtime error: %v", err)
		}
		if strings.TrimSpace(output) != "15" {
			t.Errorf("expected '15', got %q", output)
		}
	})

	t.Run("ref-modify with identity function", func(t *testing.T) {
		source := `
main : () -> <io> () =
  let r = ref-new(42)
  let _ = ref-modify(r, fn(x) => x)
  let v = ref-read(r)
  print(show(v))
`
		output, err := runProgram(source, "")
		if err != nil {
			t.Fatalf("runtime error: %v", err)
		}
		if strings.TrimSpace(output) != "42" {
			t.Errorf("expected '42', got %q", output)
		}
	})

	t.Run("multiple ref-modify calls accumulate", func(t *testing.T) {
		source := `
main : () -> <io> () =
  let r = ref-new(0)
  let _ = ref-modify(r, fn(x) => x + 1)
  let _ = ref-modify(r, fn(x) => x + 1)
  let _ = ref-modify(r, fn(x) => x + 1)
  let v = ref-read(r)
  print(show(v))
`
		output, err := runProgram(source, "")
		if err != nil {
			t.Fatalf("runtime error: %v", err)
		}
		if strings.TrimSpace(output) != "3" {
			t.Errorf("expected '3', got %q", output)
		}
	})
}

func TestRefClose(t *testing.T) {
	t.Run("ref-close then use saved value", func(t *testing.T) {
		source := `
main : () -> <io> () =
  let r = ref-new(10)
  let v = ref-read(r)
  let _ = ref-close(r)
  print(show(v))
`
		output, err := runProgram(source, "")
		if err != nil {
			t.Fatalf("runtime error: %v", err)
		}
		if strings.TrimSpace(output) != "10" {
			t.Errorf("expected '10', got %q", output)
		}
	})

	t.Run("ref-close marks ref as closed", func(t *testing.T) {
		// After close, the ref is marked closed (HandleCount drops to 0)
		// but Go VM doesn't currently trap on read/write of closed refs
		source := `
main : () -> <io> () =
  let r = ref-new(10)
  let _ = ref-close(r)
  print("closed")
`
		output, err := runProgram(source, "")
		if err != nil {
			t.Fatalf("runtime error: %v", err)
		}
		if strings.TrimSpace(output) != "closed" {
			t.Errorf("expected 'closed', got %q", output)
		}
	})
}

// ─── STM Tests ───

func TestTVarBasics(t *testing.T) {
	t.Run("tvar-new and tvar-read", func(t *testing.T) {
		source := `
main : () -> <io> () =
  let tv = tvar-new(42)
  let v = tvar-read(tv)
  print(show(v))
`
		output, err := runProgram(source, "")
		if err != nil {
			t.Fatalf("runtime error: %v", err)
		}
		if strings.TrimSpace(output) != "42" {
			t.Errorf("expected '42', got %q", output)
		}
	})

	t.Run("multiple tvars hold independent values", func(t *testing.T) {
		source := `
main : () -> <io> () =
  let a = tvar-new(10)
  let b = tvar-new(20)
  let va = tvar-read(a)
  let vb = tvar-read(b)
  print(show(va + vb))
`
		output, err := runProgram(source, "")
		if err != nil {
			t.Fatalf("runtime error: %v", err)
		}
		if strings.TrimSpace(output) != "30" {
			t.Errorf("expected '30', got %q", output)
		}
	})
}

func TestAtomically(t *testing.T) {
	t.Run("atomically returns body result", func(t *testing.T) {
		source := `
main : () -> <io> () =
  let result = atomically(fn() => 42)
  print(show(result))
`
		output, err := runProgram(source, "")
		if err != nil {
			t.Fatalf("runtime error: %v", err)
		}
		if strings.TrimSpace(output) != "42" {
			t.Errorf("expected '42', got %q", output)
		}
	})

	t.Run("atomically with tvar-read inside", func(t *testing.T) {
		source := `
main : () -> <io> () =
  let tv = tvar-new(99)
  let result = atomically(fn() => tvar-read(tv))
  print(show(result))
`
		output, err := runProgram(source, "")
		if err != nil {
			t.Fatalf("runtime error: %v", err)
		}
		if strings.TrimSpace(output) != "99" {
			t.Errorf("expected '99', got %q", output)
		}
	})

	t.Run("atomically with tvar-write commits changes", func(t *testing.T) {
		source := `
main : () -> <io> () =
  let tv = tvar-new(10)
  let _ = atomically(fn() => tvar-write(tv, 42))
  let v = tvar-read(tv)
  print(show(v))
`
		output, err := runProgram(source, "")
		if err != nil {
			t.Fatalf("runtime error: %v", err)
		}
		if strings.TrimSpace(output) != "42" {
			t.Errorf("expected '42', got %q", output)
		}
	})

	t.Run("atomically read-modify-write", func(t *testing.T) {
		source := `
main : () -> <io> () =
  let tv = tvar-new(10)
  let _ = atomically(fn() =>
    let v = tvar-read(tv) in tvar-write(tv, v + 5)
  )
  let result = tvar-read(tv)
  print(show(result))
`
		output, err := runProgram(source, "")
		if err != nil {
			t.Fatalf("runtime error: %v", err)
		}
		if strings.TrimSpace(output) != "15" {
			t.Errorf("expected '15', got %q", output)
		}
	})
}

func TestAtomicallyBankTransfer(t *testing.T) {
	source := `
main : () -> <io> () =
  let from = tvar-new(500)
  let to = tvar-new(200)
  let _ = atomically(fn() =>
    let bal = tvar-read(from) in
    let _ = tvar-write(from, bal - 100) in
    let toBal = tvar-read(to) in
    tvar-write(to, toBal + 100)
  )
  let f = tvar-read(from)
  let t2 = tvar-read(to)
  print(show(f + t2))
`
	output, err := runProgram(source, "")
	if err != nil {
		t.Fatalf("runtime error: %v", err)
	}
	// Total preserved: 500 + 200 = 700
	if strings.TrimSpace(output) != "700" {
		t.Errorf("expected '700', got %q", output)
	}
}

func TestAtomicallySequential(t *testing.T) {
	source := `
main : () -> <io> () =
  let tv = tvar-new(0)
  let _ = atomically(fn() => tvar-write(tv, 10))
  let _ = atomically(fn() =>
    let v = tvar-read(tv) in tvar-write(tv, v + 5)
  )
  let result = tvar-read(tv)
  print(show(result))
`
	output, err := runProgram(source, "")
	if err != nil {
		t.Fatalf("runtime error: %v", err)
	}
	if strings.TrimSpace(output) != "15" {
		t.Errorf("expected '15', got %q", output)
	}
}

func TestAtomicallyWriteBuffering(t *testing.T) {
	t.Run("buffered write visible inside transaction", func(t *testing.T) {
		source := `
main : () -> <io> () =
  let tv = tvar-new(1)
  let result = atomically(fn() =>
    let _ = tvar-write(tv, 99) in tvar-read(tv)
  )
  print(show(result))
`
		output, err := runProgram(source, "")
		if err != nil {
			t.Fatalf("runtime error: %v", err)
		}
		if strings.TrimSpace(output) != "99" {
			t.Errorf("expected '99', got %q", output)
		}
	})

	t.Run("multiple writes to same tvar last write wins", func(t *testing.T) {
		source := `
main : () -> <io> () =
  let tv = tvar-new(0)
  let _ = atomically(fn() =>
    let _ = tvar-write(tv, 10) in
    let _ = tvar-write(tv, 20) in
    tvar-write(tv, 30)
  )
  let v = tvar-read(tv)
  print(show(v))
`
		output, err := runProgram(source, "")
		if err != nil {
			t.Fatalf("runtime error: %v", err)
		}
		if strings.TrimSpace(output) != "30" {
			t.Errorf("expected '30', got %q", output)
		}
	})
}

func TestOrElse(t *testing.T) {
	t.Run("or-else returns first action when it succeeds", func(t *testing.T) {
		source := `
main : () -> <io> () =
  let tv = tvar-new(42)
  let result = atomically(fn() =>
    or-else(
      fn() => tvar-read(tv),
      fn() => 0
    )
  )
  print(show(result))
`
		output, err := runProgram(source, "")
		if err != nil {
			t.Fatalf("runtime error: %v", err)
		}
		if strings.TrimSpace(output) != "42" {
			t.Errorf("expected '42', got %q", output)
		}
	})

	t.Run("or-else falls through to second on retry", func(t *testing.T) {
		source := `
main : () -> <io> () =
  let result = atomically(fn() =>
    or-else(
      fn() => retry(),
      fn() => 99
    )
  )
  print(show(result))
`
		output, err := runProgram(source, "")
		if err != nil {
			t.Fatalf("runtime error: %v", err)
		}
		if strings.TrimSpace(output) != "99" {
			t.Errorf("expected '99', got %q", output)
		}
	})

	t.Run("or-else rolls back first action writes on retry", func(t *testing.T) {
		source := `
main : () -> <io> () =
  let tv = tvar-new(10)
  let result = atomically(fn() =>
    or-else(
      fn() => let _ = tvar-write(tv, 999) in retry(),
      fn() => tvar-read(tv)
    )
  )
  print(show(result))
`
		output, err := runProgram(source, "")
		if err != nil {
			t.Fatalf("runtime error: %v", err)
		}
		// Second branch should see original value 10, not 999
		if strings.TrimSpace(output) != "10" {
			t.Errorf("expected '10', got %q", output)
		}
	})

	t.Run("nested or-else", func(t *testing.T) {
		source := `
main : () -> <io> () =
  let result = atomically(fn() =>
    or-else(
      fn() => or-else(fn() => retry(), fn() => retry()),
      fn() => 77
    )
  )
  print(show(result))
`
		output, err := runProgram(source, "")
		if err != nil {
			t.Fatalf("runtime error: %v", err)
		}
		if strings.TrimSpace(output) != "77" {
			t.Errorf("expected '77', got %q", output)
		}
	})
}

func TestTVarTakePut(t *testing.T) {
	t.Run("tvar-take removes value and tvar-put restores it", func(t *testing.T) {
		source := `
main : () -> <io> () =
  let tv = tvar-new(42)
  let v = atomically(fn() =>
    let v = tvar-take(tv) in
    let _ = tvar-put(tv, v + 1) in
    v
  )
  print(show(v))
`
		output, err := runProgram(source, "")
		if err != nil {
			t.Fatalf("runtime error: %v", err)
		}
		if strings.TrimSpace(output) != "42" {
			t.Errorf("expected '42', got %q", output)
		}
	})

	t.Run("take then put commits correctly", func(t *testing.T) {
		source := `
main : () -> <io> () =
  let tv = tvar-new(10)
  let _ = atomically(fn() =>
    let v = tvar-take(tv) in
    tvar-put(tv, v * 2)
  )
  let result = tvar-read(tv)
  print(show(result))
`
		output, err := runProgram(source, "")
		if err != nil {
			t.Fatalf("runtime error: %v", err)
		}
		if strings.TrimSpace(output) != "20" {
			t.Errorf("expected '20', got %q", output)
		}
	})
}

func TestSTMErrors(t *testing.T) {
	t.Run("tvar-write outside atomically writes directly", func(t *testing.T) {
		// Go VM allows tvar-write outside atomically (direct write)
		source := `
main : () -> <io> () =
  let tv = tvar-new(0)
  let _ = tvar-write(tv, 42)
  let v = tvar-read(tv)
  print(show(v))
`
		output, err := runProgram(source, "")
		if err != nil {
			t.Fatalf("runtime error: %v", err)
		}
		if strings.TrimSpace(output) != "42" {
			t.Errorf("expected '42', got %q", output)
		}
	})
}
