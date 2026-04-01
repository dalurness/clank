package compiler

import "fmt"

// OpcodeEntry documents a registered opcode.
type OpcodeEntry struct {
	Name  string
	Value byte
	Group string // functional group (e.g. "stack", "arithmetic", "stm")
}

// OpcodeRegistry returns the authoritative list of all allocated opcodes.
// Any new opcode MUST be added here AND to the const block in opcode.go.
// The TestOpcodeRegistryNoDuplicates test will catch collisions.
func OpcodeRegistry() []OpcodeEntry {
	return []OpcodeEntry{
		// Stack manipulation (0x00-0x07)
		{"NOP", OpNOP, "stack"},
		{"DUP", OpDUP, "stack"},
		{"DROP", OpDROP, "stack"},
		{"SWAP", OpSWAP, "stack"},
		{"ROT", OpROT, "stack"},
		{"OVER", OpOVER, "stack"},
		{"PICK", OpPICK, "stack"},
		{"ROLL", OpROLL, "stack"},

		// Constants / Literals (0x10-0x18)
		{"PUSH_INT", OpPUSH_INT, "literal"},
		{"PUSH_INT16", OpPUSH_INT16, "literal"},
		{"PUSH_INT32", OpPUSH_INT32, "literal"},
		{"PUSH_TRUE", OpPUSH_TRUE, "literal"},
		{"PUSH_FALSE", OpPUSH_FALSE, "literal"},
		{"PUSH_UNIT", OpPUSH_UNIT, "literal"},
		{"PUSH_STR", OpPUSH_STR, "literal"},
		{"PUSH_BYTE", OpPUSH_BYTE, "literal"},
		{"PUSH_RAT", OpPUSH_RAT, "literal"},

		// Arithmetic (0x20-0x25)
		{"ADD", OpADD, "arithmetic"},
		{"SUB", OpSUB, "arithmetic"},
		{"MUL", OpMUL, "arithmetic"},
		{"DIV", OpDIV, "arithmetic"},
		{"MOD", OpMOD, "arithmetic"},
		{"NEG", OpNEG, "arithmetic"},

		// Comparison (0x28-0x2D)
		{"EQ", OpEQ, "comparison"},
		{"NEQ", OpNEQ, "comparison"},
		{"LT", OpLT, "comparison"},
		{"GT", OpGT, "comparison"},
		{"LTE", OpLTE, "comparison"},
		{"GTE", OpGTE, "comparison"},

		// Logic (0x30-0x32)
		{"AND", OpAND, "logic"},
		{"OR", OpOR, "logic"},
		{"NOT", OpNOT, "logic"},

		// Control flow (0x38-0x3F)
		{"JMP", OpJMP, "control"},
		{"JMP_IF", OpJMP_IF, "control"},
		{"JMP_UNLESS", OpJMP_UNLESS, "control"},
		{"CALL", OpCALL, "control"},
		{"CALL_DYN", OpCALL_DYN, "control"},
		{"RET", OpRET, "control"},
		{"TAIL_CALL", OpTAIL_CALL, "control"},
		{"TAIL_CALL_DYN", OpTAIL_CALL_DYN, "control"},

		// Closures & Quotes (0x40-0x41)
		{"QUOTE", OpQUOTE, "closure"},
		{"CLOSURE", OpCLOSURE, "closure"},

		// Local variables (0x48-0x49)
		{"LOCAL_GET", OpLOCAL_GET, "local"},
		{"LOCAL_SET", OpLOCAL_SET, "local"},

		// Collections (0x50-0x61)
		{"LIST_NEW", OpLIST_NEW, "collection"},
		{"LIST_LEN", OpLIST_LEN, "collection"},
		{"LIST_HEAD", OpLIST_HEAD, "collection"},
		{"LIST_TAIL", OpLIST_TAIL, "collection"},
		{"LIST_CONS", OpLIST_CONS, "collection"},
		{"LIST_CAT", OpLIST_CAT, "collection"},
		{"LIST_IDX", OpLIST_IDX, "collection"},
		{"LIST_REV", OpLIST_REV, "collection"},
		{"TUPLE_NEW", OpTUPLE_NEW, "collection"},
		{"TUPLE_GET", OpTUPLE_GET, "collection"},
		{"RECORD_NEW", OpRECORD_NEW, "collection"},
		{"RECORD_GET", OpRECORD_GET, "collection"},
		{"RECORD_SET", OpRECORD_SET, "collection"},
		{"RECORD_REST", OpRECORD_REST, "collection"},
		{"UNION_NEW", OpUNION_NEW, "collection"},
		{"VARIANT_TAG", OpVARIANT_TAG, "collection"},
		{"VARIANT_FIELD", OpVARIANT_FIELD, "collection"},
		{"TUPLE_GET_DYN", OpTUPLE_GET_DYN, "collection"},

		// Strings (0x62-0x67)
		{"STR_CAT", OpSTR_CAT, "string"},
		{"STR_LEN", OpSTR_LEN, "string"},
		{"STR_SPLIT", OpSTR_SPLIT, "string"},
		{"STR_JOIN", OpSTR_JOIN, "string"},
		{"STR_TRIM", OpSTR_TRIM, "string"},
		{"TO_STR", OpTO_STR, "string"},

		// Effect handling (0x70-0x74)
		{"HANDLE_PUSH", OpHANDLE_PUSH, "effect"},
		{"HANDLE_POP", OpHANDLE_POP, "effect"},
		{"EFFECT_PERFORM", OpEFFECT_PERFORM, "effect"},
		{"RESUME", OpRESUME, "effect"},
		{"RESUME_DISCARD", OpRESUME_DISCARD, "effect"},

		// I/O (0x90)
		{"IO_PRINT", OpIO_PRINT, "io"},

		// Async / Concurrency (0xA0-0xAF)
		{"TASK_SPAWN", OpTASK_SPAWN, "async"},
		{"TASK_AWAIT", OpTASK_AWAIT, "async"},
		{"TASK_YIELD", OpTASK_YIELD, "async"},
		{"TASK_SLEEP", OpTASK_SLEEP, "async"},
		{"TASK_GROUP_ENTER", OpTASK_GROUP_ENTER, "async"},
		{"TASK_GROUP_EXIT", OpTASK_GROUP_EXIT, "async"},
		{"CHAN_NEW", OpCHAN_NEW, "async"},
		{"CHAN_SEND", OpCHAN_SEND, "async"},
		{"CHAN_RECV", OpCHAN_RECV, "async"},
		{"CHAN_TRY_RECV", OpCHAN_TRY_RECV, "async"},
		{"CHAN_CLOSE", OpCHAN_CLOSE, "async"},
		{"SELECT_BUILD", OpSELECT_BUILD, "async"},
		{"SELECT_WAIT", OpSELECT_WAIT, "async"},
		{"TASK_CANCEL_CHECK", OpTASK_CANCEL_CHECK, "async"},
		{"TASK_SHIELD_ENTER", OpTASK_SHIELD_ENTER, "async"},
		{"TASK_SHIELD_EXIT", OpTASK_SHIELD_EXIT, "async"},

		// Dispatch (0xB0)
		{"DISPATCH", OpDISPATCH, "dispatch"},

		// Iterators (0xB1-0xB3)
		{"ITER_NEW", OpITER_NEW, "iterator"},
		{"ITER_NEXT", OpITER_NEXT, "iterator"},
		{"ITER_CLOSE", OpITER_CLOSE, "iterator"},

		// STM (0xC0-0xC4)
		{"TVAR_NEW", OpTVAR_NEW, "stm"},
		{"TVAR_READ", OpTVAR_READ, "stm"},
		{"TVAR_WRITE", OpTVAR_WRITE, "stm"},
		{"TVAR_TAKE", OpTVAR_TAKE, "stm"},
		{"TVAR_PUT", OpTVAR_PUT, "stm"},

		// Ref (0xD0-0xD5)
		{"REF_NEW", OpREF_NEW, "ref"},
		{"REF_READ", OpREF_READ, "ref"},
		{"REF_WRITE", OpREF_WRITE, "ref"},
		{"REF_CAS", OpREF_CAS, "ref"},
		{"REF_MODIFY", OpREF_MODIFY, "ref"},
		{"REF_CLOSE", OpREF_CLOSE, "ref"},

		// Foreign (0xE0)
		{"CALL_EXTERN", OpCALL_EXTERN, "foreign"},

		// System (0xF0-0xF2)
		{"HALT", OpHALT, "system"},
		{"TRAP", OpTRAP, "system"},
		{"DEBUG", OpDEBUG, "system"},
	}
}

// ValidateOpcodeRegistry checks for duplicate byte values and returns errors.
func ValidateOpcodeRegistry() []string {
	registry := OpcodeRegistry()
	seen := make(map[byte]string) // byte value → first opcode name
	var errors []string
	for _, entry := range registry {
		if prev, exists := seen[entry.Value]; exists {
			errors = append(errors, fmt.Sprintf(
				"opcode collision: %s (0x%02X) conflicts with %s",
				entry.Name, entry.Value, prev))
		}
		seen[entry.Value] = entry.Name
	}
	return errors
}
