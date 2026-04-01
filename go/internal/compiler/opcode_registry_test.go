package compiler

import (
	"testing"
)

func TestOpcodeRegistryNoDuplicates(t *testing.T) {
	errors := ValidateOpcodeRegistry()
	for _, err := range errors {
		t.Errorf("%s", err)
	}
}

func TestOpcodeRegistryCoversAllConstants(t *testing.T) {
	// Verify that the registry has the expected number of entries.
	// Update this count when adding new opcodes.
	registry := OpcodeRegistry()
	if len(registry) != 109 {
		t.Errorf("opcode registry has %d entries, expected 109 — did you add a new opcode to opcode.go without registering it?", len(registry))
	}
}

func TestOpcodeRegistryMatchesConstants(t *testing.T) {
	// Spot-check that registry values match the const block.
	checks := map[string]byte{
		"NOP":        0x00,
		"HALT":       0xF0,
		"TVAR_NEW":   0xC0,
		"CALL":       0x3B,
		"IO_PRINT":   0x90,
		"DISPATCH":   0xB0,
		"CALL_EXTERN": 0xE0,
	}
	registry := OpcodeRegistry()
	byName := make(map[string]byte)
	for _, e := range registry {
		byName[e.Name] = e.Value
	}
	for name, expected := range checks {
		if got, ok := byName[name]; !ok {
			t.Errorf("opcode %s not found in registry", name)
		} else if got != expected {
			t.Errorf("opcode %s: got 0x%02X, expected 0x%02X", name, got, expected)
		}
	}
}
