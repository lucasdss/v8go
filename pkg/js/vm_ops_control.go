// vm_ops_control.go — Control flow bytecode handlers.
package js

// --- Control flow handlers ---

func opJump(vm *VM, frame *VMFrame, instr Instruction) {
	frame.PC = int(instr.OperandA)
}

func opJumpIfFalse(vm *VM, frame *VMFrame, instr Instruction) {
	if !frame.Acc.IsTruthy() {
		frame.PC = int(instr.OperandA)
	}
}

func opJumpIfTrue(vm *VM, frame *VMFrame, instr Instruction) {
	if frame.Acc.IsTruthy() {
		frame.PC = int(instr.OperandA)
	}
}

func opJumpIfToBooleanTrue(vm *VM, frame *VMFrame, instr Instruction) {
	if frame.Acc.IsTruthy() {
		frame.PC = int(instr.OperandA)
	}
}

func opJumpIfToBooleanFalse(vm *VM, frame *VMFrame, instr Instruction) {
	if !frame.Acc.IsTruthy() {
		frame.PC = int(instr.OperandA)
	}
}

func opJumpIfNotNullish(vm *VM, frame *VMFrame, instr Instruction) {
	if !frame.Acc.IsNull() && !frame.Acc.IsUndefined() {
		frame.PC = int(instr.OperandA)
	}
}

func opReturn(vm *VM, frame *VMFrame, instr Instruction) {
	// Pop this function's name from the call stack (pushed in opCall/opCallFast).
	vm.popCallName()

	// OperandB==1 is set by the peephole optimizer to indicate OperandA
	// holds a constant pool index that should be loaded before returning.
	if instr.OperandB == 1 {
		idx := int(instr.OperandA)
		if idx < len(frame.Func.Constants) {
			frame.Acc = frame.Func.Constants[idx]
		}
	}
	// OperandB==2: return undefined (merged LdaUndefined+Return).
	if instr.OperandB == 2 {
		frame.Acc = Undefined
	}
	// Generator return: mark done.
	if frame.GenState != nil {
		frame.GenState.Done = true
		// Build result object {value: acc, done: true}.
		resultObj := NewJSObject()
		resultObj.Set("value", frame.Acc)
		resultObj.Set("done", True)
		frame.Acc = NewObject(resultObj)
	}
	// Finally interception: jump to finally instead of returning.
	if frame.FinallyPC >= 0 {
		frame.SavedAcc = frame.Acc
		frame.PC = frame.FinallyPC
		frame.FinallyPC = -1
		return // don't set ShouldReturn — let execution continue at finally
	}
	frame.ShouldReturn = true
}

func opThrow(vm *VM, frame *VMFrame, instr Instruction) {
	frame.Thrown = frame.Acc
	if frame.HandlerPC >= 0 {
		frame.PC = frame.HandlerPC
		// Don't reset HandlerPC — let execute/executeFrame loop consume it.
		// Don't set ShouldReturn — let executeOne detect Thrown != Undefined.
		return
	}
	// Unhandled exception: check finally handler.
	if frame.FinallyPC >= 0 {
		frame.PC = frame.FinallyPC
		// Don't reset FinallyPC yet — executeFrame will consume it.
		return
	}
	frame.ShouldReturn = true
}

func opSetTryHandler(vm *VM, frame *VMFrame, instr Instruction) {
	frame.HandlerPC = int(instr.OperandA)
}

func opClearTryHandler(vm *VM, frame *VMFrame, instr Instruction) {
	frame.HandlerPC = -1
	frame.Thrown = Undefined
}

func opSetFinallyHandler(vm *VM, frame *VMFrame, instr Instruction) {
	// OperandA=255 signals "clear/disable" the finally handler.
	if instr.OperandA == 255 {
		frame.FinallyPC = -1
		return
	}
	frame.FinallyPC = int(instr.OperandA)
}

func opForInSetup(vm *VM, frame *VMFrame, instr Instruction) {
	objReg := int(instr.OperandA)
	if objReg < len(frame.Regs) && frame.Regs[objReg].IsObject() && frame.Regs[objReg].ObjVal != nil {
		obj := frame.Regs[objReg].ObjVal
		frame.forInObj = obj
		frame.forInKeys = nil
		if !obj.Shape.IsDictionary {
			for name, entry := range obj.Shape.Properties {
				if entry.Attr&AttrEnumerable != 0 {
					frame.forInKeys = append(frame.forInKeys, name)
				}
			}
		}
		if obj.Shape.IsDictionary && obj.Dictionary != nil {
			for name := range obj.Dictionary {
				frame.forInKeys = append(frame.forInKeys, name)
			}
		}
		frame.forInIndex = 0
		if len(frame.forInKeys) > 0 {
			frame.Acc = NewString(frame.forInKeys[0])
			frame.forInIndex = 1
		} else {
			frame.Acc = Undefined
		}
	} else {
		frame.Acc = Undefined
	}
}

func opForInNext(vm *VM, frame *VMFrame, instr Instruction) {
	if frame.forInObj != nil && frame.forInIndex < len(frame.forInKeys) {
		frame.Acc = NewString(frame.forInKeys[frame.forInIndex])
		frame.forInIndex++
	} else {
		frame.Acc = Undefined
		frame.forInObj = nil
		frame.forInKeys = nil
	}
}
