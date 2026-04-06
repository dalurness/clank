package compiler

// BytecodeWord is a single compiled word (function) in the bytecode module.
type BytecodeWord struct {
	Name       string
	WordID     int
	Code       []byte
	LocalCount int
	IsPublic   bool
}

// BytecodeModule is the in-memory representation of a compiled program.
type BytecodeModule struct {
	Words         []BytecodeWord
	Strings       []string
	Rationals     []float64
	VariantNames  []string            // maps variant tag → name
	EntryWordID   *int                // nil if no main
	DispatchTable map[string]map[string]int // method → typeTag → wordID
}
