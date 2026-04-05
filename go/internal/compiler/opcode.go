// Package compiler translates a desugared Clank AST into stack-based VM
// bytecode as specified in plan/features/vm-instruction-set.md.
package compiler

// Op enumerates all VM opcodes.
const (
	// Stack manipulation
	OpNOP  byte = 0x00
	OpDUP  byte = 0x01
	OpDROP byte = 0x02
	OpSWAP byte = 0x03
	OpROT  byte = 0x04
	OpOVER byte = 0x05
	OpPICK byte = 0x06
	OpROLL byte = 0x07

	// Constants / Literals
	OpPUSH_INT   byte = 0x10
	OpPUSH_INT16 byte = 0x11
	OpPUSH_INT32 byte = 0x12
	OpPUSH_TRUE  byte = 0x13
	OpPUSH_FALSE byte = 0x14
	OpPUSH_UNIT  byte = 0x15
	OpPUSH_STR   byte = 0x16
	OpPUSH_BYTE  byte = 0x17
	OpPUSH_RAT   byte = 0x18

	// Arithmetic
	OpADD byte = 0x20
	OpSUB byte = 0x21
	OpMUL byte = 0x22
	OpDIV byte = 0x23
	OpMOD byte = 0x24
	OpNEG byte = 0x25

	// Comparison
	OpEQ  byte = 0x28
	OpNEQ byte = 0x29
	OpLT  byte = 0x2A
	OpGT  byte = 0x2B
	OpLTE byte = 0x2C
	OpGTE byte = 0x2D

	// Logic
	OpAND byte = 0x30
	OpOR  byte = 0x31
	OpNOT byte = 0x32

	// Control flow
	OpJMP          byte = 0x38
	OpJMP_IF       byte = 0x39
	OpJMP_UNLESS   byte = 0x3A
	OpCALL         byte = 0x3B
	OpCALL_DYN     byte = 0x3C
	OpRET          byte = 0x3D
	OpTAIL_CALL    byte = 0x3E
	OpTAIL_CALL_DYN byte = 0x3F

	// Closures & Quotes
	OpQUOTE   byte = 0x40
	OpCLOSURE byte = 0x41

	// Local variables
	OpLOCAL_GET byte = 0x48
	OpLOCAL_SET byte = 0x49

	// Collections
	OpLIST_NEW     byte = 0x50
	OpLIST_LEN     byte = 0x51
	OpLIST_HEAD    byte = 0x52
	OpLIST_TAIL    byte = 0x53
	OpLIST_CONS    byte = 0x54
	OpLIST_CAT     byte = 0x55
	OpLIST_IDX     byte = 0x56
	OpLIST_REV     byte = 0x57
	OpTUPLE_NEW    byte = 0x58
	OpTUPLE_GET    byte = 0x59
	OpRECORD_NEW   byte = 0x5A
	OpRECORD_GET   byte = 0x5B
	OpRECORD_SET   byte = 0x5C
	OpRECORD_REST  byte = 0x5D
	OpUNION_NEW    byte = 0x5E
	OpVARIANT_TAG  byte = 0x5F
	OpVARIANT_FIELD byte = 0x60
	OpTUPLE_GET_DYN byte = 0x61

	// Strings
	OpSTR_CAT   byte = 0x62
	OpSTR_LEN   byte = 0x63
	OpSTR_SPLIT byte = 0x64
	OpSTR_JOIN  byte = 0x65
	OpSTR_TRIM  byte = 0x66
	OpTO_STR    byte = 0x67

	// Effect handling
	OpHANDLE_PUSH    byte = 0x70
	OpHANDLE_POP     byte = 0x71
	OpEFFECT_PERFORM byte = 0x72
	OpRESUME         byte = 0x73
	OpRESUME_DISCARD byte = 0x74

	// I/O
	OpIO_PRINT byte = 0x90

	// Async / Concurrency
	OpTASK_SPAWN        byte = 0xA0
	OpTASK_AWAIT        byte = 0xA1
	OpTASK_YIELD        byte = 0xA2
	OpTASK_SLEEP        byte = 0xA3
	OpTASK_GROUP_ENTER  byte = 0xA4
	OpTASK_GROUP_EXIT   byte = 0xA5
	OpCHAN_NEW          byte = 0xA6
	OpCHAN_SEND         byte = 0xA7
	OpCHAN_RECV         byte = 0xA8
	OpCHAN_TRY_RECV     byte = 0xA9
	OpCHAN_CLOSE        byte = 0xAA
	OpSELECT_BUILD      byte = 0xAB
	OpSELECT_WAIT       byte = 0xAC
	OpTASK_CANCEL_CHECK byte = 0xAD
	OpTASK_SHIELD_ENTER byte = 0xAE
	OpTASK_SHIELD_EXIT  byte = 0xAF

	// Dispatch
	OpDISPATCH byte = 0xB0

	// Iterators
	OpITER_NEW   byte = 0xB1
	OpITER_NEXT  byte = 0xB2
	OpITER_CLOSE byte = 0xB3

	// STM
	OpTVAR_NEW   byte = 0xC0
	OpTVAR_READ  byte = 0xC1
	OpTVAR_WRITE byte = 0xC2
	OpTVAR_TAKE  byte = 0xC3
	OpTVAR_PUT   byte = 0xC4

	// Ref
	OpREF_NEW    byte = 0xD0
	OpREF_READ   byte = 0xD1
	OpREF_WRITE  byte = 0xD2
	OpREF_CAS    byte = 0xD3
	OpREF_MODIFY byte = 0xD4
	OpREF_CLOSE  byte = 0xD5

	// System
	OpHALT  byte = 0xF0
	OpTRAP  byte = 0xF1
	OpDEBUG byte = 0xF2
)
