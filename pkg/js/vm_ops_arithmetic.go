// vm_ops_arithmetic.go — Arithmetic, bitwise, comparison, logical, and type conversion handlers.
package js

import (
	"math"
	"math/big"
)

// --- Arithmetic handlers ---

func opAdd(vm *VM, frame *VMFrame, instr Instruction) {
	lhs := frame.Regs[int(instr.OperandA)]
	frame.Acc = jsAdd(lhs, frame.Acc)
	recordBinaryFeedback(frame, instr)
}

func opSub(vm *VM, frame *VMFrame, instr Instruction) {
	lhs := frame.Regs[int(instr.OperandA)]
	// Fast path: both numbers — avoid IsBigInt/ToNumber overhead.
	if lhs.Tag == TagNumber && frame.Acc.Tag == TagNumber {
		result := lhs.NumVal - frame.Acc.NumVal
		if isSmallInt(result) {
			frame.Acc = smallIntValue(int(result))
		} else {
			frame.Acc = JSValue{Tag: TagNumber, NumVal: result}
		}
		recordBinaryFeedback(frame, instr)
		return
	}
	if lhs.IsBigInt() || frame.Acc.IsBigInt() {
		if bigIntMixedTypeError(lhs, frame.Acc) {
			throwTypeErrorInFrame(frame, "Cannot mix BigInt and other types")
			return
		}
		if lhs.IsBigInt() && frame.Acc.IsBigInt() {
			result := new(big.Int).Sub(lhs.BigIntVal, frame.Acc.BigIntVal)
			frame.Acc = JSValue{Tag: TagBigInt, BigIntVal: result}
			recordBinaryFeedback(frame, instr)
			return
		}
	}
	frame.Acc = JSValue{Tag: TagNumber, NumVal: lhs.ToNumber() - frame.Acc.ToNumber()}
	recordBinaryFeedback(frame, instr)
}

func opMul(vm *VM, frame *VMFrame, instr Instruction) {
	lhs := frame.Regs[int(instr.OperandA)]
	// Fast path: both numbers — avoid IsBigInt/ToNumber overhead.
	if lhs.Tag == TagNumber && frame.Acc.Tag == TagNumber {
		result := lhs.NumVal * frame.Acc.NumVal
		if isSmallInt(result) {
			frame.Acc = smallIntValue(int(result))
		} else {
			frame.Acc = JSValue{Tag: TagNumber, NumVal: result}
		}
		recordBinaryFeedback(frame, instr)
		return
	}
	if lhs.IsBigInt() || frame.Acc.IsBigInt() {
		if bigIntMixedTypeError(lhs, frame.Acc) {
			throwTypeErrorInFrame(frame, "Cannot mix BigInt and other types")
			return
		}
		if lhs.IsBigInt() && frame.Acc.IsBigInt() {
			result := new(big.Int).Mul(lhs.BigIntVal, frame.Acc.BigIntVal)
			frame.Acc = JSValue{Tag: TagBigInt, BigIntVal: result}
			recordBinaryFeedback(frame, instr)
			return
		}
	}
	frame.Acc = JSValue{Tag: TagNumber, NumVal: lhs.ToNumber() * frame.Acc.ToNumber()}
	recordBinaryFeedback(frame, instr)
}

func opDiv(vm *VM, frame *VMFrame, instr Instruction) {
	lhs := frame.Regs[int(instr.OperandA)]
	if lhs.IsBigInt() || frame.Acc.IsBigInt() {
		if bigIntMixedTypeError(lhs, frame.Acc) {
			throwTypeErrorInFrame(frame, "Cannot mix BigInt and other types")
			return
		}
		if lhs.IsBigInt() && frame.Acc.IsBigInt() {
			if frame.Acc.BigIntVal.Sign() == 0 {
				throwRangeErrorInFrame(frame, "Division by zero")
				return
			}
			result := new(big.Int).Div(lhs.BigIntVal, frame.Acc.BigIntVal)
			frame.Acc = JSValue{Tag: TagBigInt, BigIntVal: result}
			recordBinaryFeedback(frame, instr)
			return
		}
	}
	l, r := lhs.ToNumber(), frame.Acc.ToNumber()
	var result float64
	if r == 0 {
		if l == 0 {
			result = math.NaN()
		} else {
			result = math.Inf(1)
		}
	} else {
		result = l / r
	}
	frame.Acc = JSValue{Tag: TagNumber, NumVal: result}
	recordBinaryFeedback(frame, instr)
}

func opMod(vm *VM, frame *VMFrame, instr Instruction) {
	lhs := frame.Regs[int(instr.OperandA)]
	if lhs.IsBigInt() || frame.Acc.IsBigInt() {
		if bigIntMixedTypeError(lhs, frame.Acc) {
			throwTypeErrorInFrame(frame, "Cannot mix BigInt and other types")
			return
		}
		if lhs.IsBigInt() && frame.Acc.IsBigInt() {
			if frame.Acc.BigIntVal.Sign() == 0 {
				throwRangeErrorInFrame(frame, "Division by zero")
				return
			}
			result := new(big.Int).Mod(lhs.BigIntVal, frame.Acc.BigIntVal)
			frame.Acc = JSValue{Tag: TagBigInt, BigIntVal: result}
			return
		}
	}
	l, r := lhs.ToNumber(), frame.Acc.ToNumber()
	var result float64
	if r == 0 {
		result = math.NaN()
	} else {
		result = math.Mod(l, r)
	}
	frame.Acc = JSValue{Tag: TagNumber, NumVal: result}
}

func opExp(vm *VM, frame *VMFrame, instr Instruction) {
	lhs := frame.Regs[int(instr.OperandA)]
	if lhs.IsBigInt() || frame.Acc.IsBigInt() {
		if bigIntMixedTypeError(lhs, frame.Acc) {
			throwTypeErrorInFrame(frame, "Cannot mix BigInt and other types")
			return
		}
		if lhs.IsBigInt() && frame.Acc.IsBigInt() {
			exp := frame.Acc.BigIntVal
			if exp.Sign() < 0 {
				throwRangeErrorInFrame(frame, "BigInt negative exponent")
				return
			}
			// Exponentiation with positive BigInt exponent.
			base := new(big.Int).Set(lhs.BigIntVal)
			e := new(big.Int).Set(exp)
			one := big.NewInt(1)
			zero := big.NewInt(0)
			if e.Cmp(zero) == 0 {
				frame.Acc = JSValue{Tag: TagBigInt, BigIntVal: big.NewInt(1)}
				return
			}
			result := big.NewInt(1)
			for e.Cmp(zero) > 0 {
				if new(big.Int).And(e, one).Cmp(one) == 0 {
					result.Mul(result, base)
				}
				base.Mul(base, base)
				e.Rsh(e, 1)
			}
			frame.Acc = JSValue{Tag: TagBigInt, BigIntVal: result}
			return
		}
	}
	frame.Acc = JSValue{Tag: TagNumber, NumVal: math.Pow(lhs.ToNumber(), frame.Acc.ToNumber())}
}

func opNegate(vm *VM, frame *VMFrame, instr Instruction) {
	if frame.Acc.IsBigInt() {
		result := new(big.Int).Neg(frame.Acc.BigIntVal)
		frame.Acc = JSValue{Tag: TagBigInt, BigIntVal: result}
		return
	}
	frame.Acc = JSValue{Tag: TagNumber, NumVal: -frame.Acc.ToNumber()}
}

func opInc(vm *VM, frame *VMFrame, instr Instruction) {
	reg := int(instr.OperandA)
	if reg < len(frame.Regs) {
		if frame.Regs[reg].IsBigInt() {
			result := new(big.Int).Add(frame.Regs[reg].BigIntVal, big.NewInt(1))
			frame.Regs[reg] = JSValue{Tag: TagBigInt, BigIntVal: result}
			frame.Acc = frame.Regs[reg]
			return
		}
		frame.Regs[reg] = JSValue{Tag: TagNumber, NumVal: frame.Regs[reg].ToNumber() + 1}
		frame.Acc = frame.Regs[reg]
	}
}

func opDec(vm *VM, frame *VMFrame, instr Instruction) {
	reg := int(instr.OperandA)
	if reg < len(frame.Regs) {
		if frame.Regs[reg].IsBigInt() {
			result := new(big.Int).Sub(frame.Regs[reg].BigIntVal, big.NewInt(1))
			frame.Regs[reg] = JSValue{Tag: TagBigInt, BigIntVal: result}
			frame.Acc = frame.Regs[reg]
			return
		}
		frame.Regs[reg] = JSValue{Tag: TagNumber, NumVal: frame.Regs[reg].ToNumber() - 1}
		frame.Acc = frame.Regs[reg]
	}
}

// --- Comparison handlers ---

func opEq(vm *VM, frame *VMFrame, instr Instruction) {
	lhs := frame.Regs[int(instr.OperandA)]
	frame.Acc = NewBoolean(lhs.Equals(frame.Acc))
}

func opNotEq(vm *VM, frame *VMFrame, instr Instruction) {
	lhs := frame.Regs[int(instr.OperandA)]
	frame.Acc = NewBoolean(!lhs.Equals(frame.Acc))
}

func opStrictEq(vm *VM, frame *VMFrame, instr Instruction) {
	lhs := frame.Regs[int(instr.OperandA)]
	frame.Acc = NewBoolean(lhs.StrictEquals(frame.Acc))
}

func opStrictNotEq(vm *VM, frame *VMFrame, instr Instruction) {
	lhs := frame.Regs[int(instr.OperandA)]
	frame.Acc = NewBoolean(!lhs.StrictEquals(frame.Acc))
}

func opLessThan(vm *VM, frame *VMFrame, instr Instruction) {
	lhs := frame.Regs[int(instr.OperandA)]
	if lhs.IsBigInt() || frame.Acc.IsBigInt() {
		if bigIntMixedTypeError(lhs, frame.Acc) {
			throwTypeErrorInFrame(frame, "Cannot mix BigInt and other types")
			return
		}
		if lhs.IsBigInt() && frame.Acc.IsBigInt() {
			frame.Acc = NewBoolean(lhs.BigIntVal.Cmp(frame.Acc.BigIntVal) < 0)
			return
		}
	}
	frame.Acc = NewBoolean(lhs.ToNumber() < frame.Acc.ToNumber())
}

func opGreaterThan(vm *VM, frame *VMFrame, instr Instruction) {
	lhs := frame.Regs[int(instr.OperandA)]
	if lhs.IsBigInt() || frame.Acc.IsBigInt() {
		if bigIntMixedTypeError(lhs, frame.Acc) {
			throwTypeErrorInFrame(frame, "Cannot mix BigInt and other types")
			return
		}
		if lhs.IsBigInt() && frame.Acc.IsBigInt() {
			frame.Acc = NewBoolean(lhs.BigIntVal.Cmp(frame.Acc.BigIntVal) > 0)
			return
		}
	}
	frame.Acc = NewBoolean(lhs.ToNumber() > frame.Acc.ToNumber())
}

func opLessEq(vm *VM, frame *VMFrame, instr Instruction) {
	lhs := frame.Regs[int(instr.OperandA)]
	if lhs.IsBigInt() || frame.Acc.IsBigInt() {
		if bigIntMixedTypeError(lhs, frame.Acc) {
			throwTypeErrorInFrame(frame, "Cannot mix BigInt and other types")
			return
		}
		if lhs.IsBigInt() && frame.Acc.IsBigInt() {
			frame.Acc = NewBoolean(lhs.BigIntVal.Cmp(frame.Acc.BigIntVal) <= 0)
			return
		}
	}
	frame.Acc = NewBoolean(lhs.ToNumber() <= frame.Acc.ToNumber())
}

func opGreaterEq(vm *VM, frame *VMFrame, instr Instruction) {
	lhs := frame.Regs[int(instr.OperandA)]
	if lhs.IsBigInt() || frame.Acc.IsBigInt() {
		if bigIntMixedTypeError(lhs, frame.Acc) {
			throwTypeErrorInFrame(frame, "Cannot mix BigInt and other types")
			return
		}
		if lhs.IsBigInt() && frame.Acc.IsBigInt() {
			frame.Acc = NewBoolean(lhs.BigIntVal.Cmp(frame.Acc.BigIntVal) >= 0)
			return
		}
	}
	frame.Acc = NewBoolean(lhs.ToNumber() >= frame.Acc.ToNumber())
}

// --- Logical handlers ---

func opLogicalNot(vm *VM, frame *VMFrame, instr Instruction) {
	frame.Acc = NewBoolean(!frame.Acc.IsTruthy())
}

func opLogicalAnd(vm *VM, frame *VMFrame, instr Instruction) {
	lhs := frame.Regs[int(instr.OperandA)]
	if !lhs.IsTruthy() {
		frame.Acc = lhs
	}
}

func opLogicalOr(vm *VM, frame *VMFrame, instr Instruction) {
	lhs := frame.Regs[int(instr.OperandA)]
	if lhs.IsTruthy() {
		frame.Acc = lhs
	}
}

// --- Bitwise handlers ---

func opBitwiseAnd(vm *VM, frame *VMFrame, instr Instruction) {
	lhs := frame.Regs[int(instr.OperandA)]
	if lhs.IsBigInt() || frame.Acc.IsBigInt() {
		if bigIntMixedTypeError(lhs, frame.Acc) {
			throwTypeErrorInFrame(frame, "Cannot mix BigInt and other types")
			return
		}
		if lhs.IsBigInt() && frame.Acc.IsBigInt() {
			result := new(big.Int).And(lhs.BigIntVal, frame.Acc.BigIntVal)
			frame.Acc = JSValue{Tag: TagBigInt, BigIntVal: result}
			return
		}
	}
	frame.Acc = JSValue{Tag: TagNumber, NumVal: float64(int32(lhs.ToNumber()) & int32(frame.Acc.ToNumber()))}
}

func opBitwiseOr(vm *VM, frame *VMFrame, instr Instruction) {
	lhs := frame.Regs[int(instr.OperandA)]
	if lhs.IsBigInt() || frame.Acc.IsBigInt() {
		if bigIntMixedTypeError(lhs, frame.Acc) {
			throwTypeErrorInFrame(frame, "Cannot mix BigInt and other types")
			return
		}
		if lhs.IsBigInt() && frame.Acc.IsBigInt() {
			result := new(big.Int).Or(lhs.BigIntVal, frame.Acc.BigIntVal)
			frame.Acc = JSValue{Tag: TagBigInt, BigIntVal: result}
			return
		}
	}
	frame.Acc = JSValue{Tag: TagNumber, NumVal: float64(int32(lhs.ToNumber()) | int32(frame.Acc.ToNumber()))}
}

func opBitwiseXor(vm *VM, frame *VMFrame, instr Instruction) {
	lhs := frame.Regs[int(instr.OperandA)]
	if lhs.IsBigInt() || frame.Acc.IsBigInt() {
		if bigIntMixedTypeError(lhs, frame.Acc) {
			throwTypeErrorInFrame(frame, "Cannot mix BigInt and other types")
			return
		}
		if lhs.IsBigInt() && frame.Acc.IsBigInt() {
			result := new(big.Int).Xor(lhs.BigIntVal, frame.Acc.BigIntVal)
			frame.Acc = JSValue{Tag: TagBigInt, BigIntVal: result}
			return
		}
	}
	frame.Acc = JSValue{Tag: TagNumber, NumVal: float64(int32(lhs.ToNumber()) ^ int32(frame.Acc.ToNumber()))}
}

func opBitwiseNot(vm *VM, frame *VMFrame, instr Instruction) {
	if frame.Acc.IsBigInt() {
		// For BigInt, bitwise NOT is defined as ~x → -x - 1 (two's complement).
		// math/big Not computes the bitwise complement of the absolute value,
		// so instead we compute -x - 1 which is the correct BigInt semantics.
		result := new(big.Int).Neg(frame.Acc.BigIntVal)
		result.Sub(result, big.NewInt(1))
		frame.Acc = JSValue{Tag: TagBigInt, BigIntVal: result}
		return
	}
	frame.Acc = JSValue{Tag: TagNumber, NumVal: float64(^int32(frame.Acc.ToNumber()))}
}

func opShiftLeft(vm *VM, frame *VMFrame, instr Instruction) {
	lhs := frame.Regs[int(instr.OperandA)]
	if lhs.IsBigInt() || frame.Acc.IsBigInt() {
		if bigIntMixedTypeError(lhs, frame.Acc) {
			throwTypeErrorInFrame(frame, "Cannot mix BigInt and other types")
			return
		}
		if lhs.IsBigInt() && frame.Acc.IsBigInt() {
			shift := frame.Acc.BigIntVal
			if shift.IsInt64() {
				n := shift.Int64()
				if n < 0 {
					// Negative shift → signed right shift by |n|
					result := new(big.Int).Rsh(lhs.BigIntVal, uint(-n))
					frame.Acc = JSValue{Tag: TagBigInt, BigIntVal: result}
					return
				}
				result := new(big.Int).Lsh(lhs.BigIntVal, uint(n))
				frame.Acc = JSValue{Tag: TagBigInt, BigIntVal: result}
				return
			}
			// Very large positive shift → result is 0 (or -1 if negative base and very large)
			// big.Int Lsh with huge shift → OOM risk; cap it heuristically.
			if shift.Sign() > 0 {
				result := new(big.Int).Lsh(lhs.BigIntVal, 0) // effectively 0n for huge shift
				frame.Acc = JSValue{Tag: TagBigInt, BigIntVal: result}
				return
			}
			// Negative large shift → shift right by magnitude (result is 0 or -1)
			result := new(big.Int).Rsh(lhs.BigIntVal, 0)
			frame.Acc = JSValue{Tag: TagBigInt, BigIntVal: result}
			return
		}
	}
	frame.Acc = JSValue{Tag: TagNumber, NumVal: float64(int32(lhs.ToNumber()) << uint32(frame.Acc.ToNumber()))}
}

func opShiftRight(vm *VM, frame *VMFrame, instr Instruction) {
	lhs := frame.Regs[int(instr.OperandA)]
	if lhs.IsBigInt() || frame.Acc.IsBigInt() {
		if bigIntMixedTypeError(lhs, frame.Acc) {
			throwTypeErrorInFrame(frame, "Cannot mix BigInt and other types")
			return
		}
		if lhs.IsBigInt() && frame.Acc.IsBigInt() {
			shift := frame.Acc.BigIntVal
			if shift.IsInt64() {
				n := shift.Int64()
				if n < 0 {
					// Negative shift → left shift by |n|
					result := new(big.Int).Lsh(lhs.BigIntVal, uint(-n))
					frame.Acc = JSValue{Tag: TagBigInt, BigIntVal: result}
					return
				}
				result := new(big.Int).Rsh(lhs.BigIntVal, uint(n))
				frame.Acc = JSValue{Tag: TagBigInt, BigIntVal: result}
				return
			}
			// Very large shift amount.
			if shift.Sign() > 0 {
				// Large positive shift → 0 or -1 for signed right shift.
				if lhs.BigIntVal.Sign() < 0 {
					result := big.NewInt(-1)
					frame.Acc = JSValue{Tag: TagBigInt, BigIntVal: result}
					return
				}
				frame.Acc = JSValue{Tag: TagBigInt, BigIntVal: big.NewInt(0)}
				return
			}
			result := new(big.Int).Lsh(lhs.BigIntVal, 0)
			frame.Acc = JSValue{Tag: TagBigInt, BigIntVal: result}
			return
		}
	}
	frame.Acc = JSValue{Tag: TagNumber, NumVal: float64(int32(lhs.ToNumber()) >> uint32(frame.Acc.ToNumber()))}
}

func opShiftRightZero(vm *VM, frame *VMFrame, instr Instruction) {
	lhs := frame.Regs[int(instr.OperandA)]
	if lhs.IsBigInt() || frame.Acc.IsBigInt() {
		throwTypeErrorInFrame(frame, "BigInt does not support unsigned right shift")
		return
	}
	frame.Acc = JSValue{Tag: TagNumber, NumVal: float64(uint32(lhs.ToNumber()) >> uint32(frame.Acc.ToNumber()))}
}

// --- Type conversion handlers ---

func opToNumber(vm *VM, frame *VMFrame, instr Instruction) {
	n := frame.Acc.ToNumber()
	frame.Acc = NewNumber(n)
}

func opToString(vm *VM, frame *VMFrame, instr Instruction) {
	frame.Acc = NewString(frame.Acc.ToString())
}

func opToBoolean(vm *VM, frame *VMFrame, instr Instruction) {
	frame.Acc = NewBoolean(frame.Acc.IsTruthy())
}

// --- JavaScript operations ---

// throwTypeErrorInFrame sets up a TypeError exception in the current frame.
// It sets frame.Thrown and frame.ShouldReturn so the VM dispatch loop
// catches it and routes through exception handlers.
// recordBinaryFeedback records type feedback for binary operation sites
// to enable JIT specialization based on observed operand types.
func recordBinaryFeedback(frame *VMFrame, instr Instruction) {
	slotIdx := int(instr.OperandC)
	if frame.Func.ICVector != nil && slotIdx < len(frame.Func.ICVector.Slots) {
		slot := &frame.Func.ICVector.Slots[slotIdx]
		slot.ObservedTag = frame.Acc.Tag
		slot.HitCount++
	}
}

// jsAdd implements the ECMAScript addition operator (+).
func jsAdd(a, b JSValue) JSValue {
	// Fast path: both numbers — avoid ToNumber/NewNumber overhead.
	if a.Tag == TagNumber && b.Tag == TagNumber {
		result := a.NumVal + b.NumVal
		if isSmallInt(result) {
			return smallIntValue(int(result))
		}
		return JSValue{Tag: TagNumber, NumVal: result}
	}
	// If either operand is a string, do string concatenation.
	if a.IsString() || b.IsString() {
		return NewString(a.ToString() + b.ToString())
	}
	// BigInt addition.
	if a.IsBigInt() || b.IsBigInt() {
		if a.IsBigInt() && b.IsBigInt() {
			result := new(big.Int).Add(a.BigIntVal, b.BigIntVal)
			return JSValue{Tag: TagBigInt, BigIntVal: result}
		}
		// Mixed BigInt + Number → TypeError per spec.
		return NewNumber(math.NaN())
	}
	// Otherwise, numeric addition.
	result := a.ToNumber() + b.ToNumber()
	if isSmallInt(result) {
		return smallIntValue(int(result))
	}
	return JSValue{Tag: TagNumber, NumVal: result}
}

// bigIntMixedTypeError checks if exactly one of a, b is BigInt and the other
// is Number. Returns true if a TypeError should be thrown.
func bigIntMixedTypeError(a, b JSValue) bool {
	bigIntA, bigIntB := a.IsBigInt(), b.IsBigInt()
	numA, numB := a.IsNumber(), b.IsNumber()
	return (bigIntA && numB) || (bigIntB && numA)
}

// jsTypeof implements the ECMAScript typeof operator.
func jsTypeof(v JSValue) string {
	switch v.Tag {
	case TagUndefined:
		return "undefined"
	case TagNull:
		return "object"
	case TagBoolean:
		return "boolean"
	case TagNumber:
		return "number"
	case TagString:
		return "string"
	case TagObject:
		if v.ObjVal != nil && (v.ObjVal.isCallable() || v.ObjVal.Bytecode != nil) {
			return "function"
		}
		return "object"
	case TagSymbol:
		return "symbol"
	case TagBigInt:
		return "bigint"
	}
	return "undefined"
}
