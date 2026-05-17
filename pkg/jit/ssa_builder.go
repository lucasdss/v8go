package jit

import (
	"fmt"

	"github.com/lucasdss/v8go/pkg/js"
)

// BuildSSA constructs an SSA graph from a compiled bytecode function.
// Returns the graph with basic blocks identified, parameter nodes created,
// and all bytecode instructions lowered to SSA nodes within each block.
func BuildSSA(bf *js.BytecodeFunction) *SSAGraph {
	g := NewSSAGraph()
	blocks := buildBasicBlocks(bf)

	// Create parameter nodes (one per virtual register).
	// Each starts as an untyped constant; type refinement happens later.
	for i := 0; i < bf.NumRegisters; i++ {
		g.Params = append(g.Params, g.newNode(SSAConst, SSAAny))
	}

	g.Blocks = blocks
	if len(blocks) > 0 {
		g.Entry = blocks[0]
	}

	// Lower bytecode to SSA nodes within each basic block.
	if err := lowerBytecodeToSSA(g, bf, blocks); err != nil {
		// Return graph with best-effort nodes; caller checks for nil.
		return g
	}
	return g
}

// lowerBytecodeToSSA walks bytecode instructions in each basic block and
// translates them into SSA nodes. It tracks the current SSA value for each
// virtual register (regMap) and the accumulator (acc node).
func lowerBytecodeToSSA(g *SSAGraph, bf *js.BytecodeFunction, blocks []*SSABasicBlock) error {
	// regMap tracks the current SSA node for each virtual register.
	regMap := make([]*SSANode, bf.NumRegisters)
	// Initialize regMap with parameter nodes.
	for i := 0; i < bf.NumRegisters && i < len(g.Params); i++ {
		regMap[i] = g.Params[i]
	}
	var acc *SSANode // current accumulator value

	for _, bb := range blocks {
		for pc := bb.StartPC; pc <= bb.EndPC; pc++ {
			if pc >= len(bf.Instructions) {
				continue
			}
			instr := bf.Instructions[pc]

			var node *SSANode
			switch instr.Op {
			case js.OpLdaSmi:
				val := int8(instr.OperandA)
				node = g.newNode(SSAConst, SSAInt32)
				node.Value = js.NewNumber(float64(val))
				node.BCPC = pc
				acc = node

			case js.OpLdaZero:
				node = g.newNode(SSAConst, SSAInt32)
				node.Value = js.NewNumber(0)
				node.BCPC = pc
				acc = node

			case js.OpLdaOne:
				node = g.newNode(SSAConst, SSAInt32)
				node.Value = js.NewNumber(1)
				node.BCPC = pc
				acc = node

			case js.OpLdaUndefined, js.OpLdaNull, js.OpLdaTrue, js.OpLdaFalse:
				// These produce non-numeric values; mark as Any.
				node = g.newNode(SSAConst, SSAAny)
				node.BCPC = pc
				acc = node

			case js.OpLdar:
				regIdx := int(instr.OperandA)
				if regIdx < len(regMap) && regMap[regIdx] != nil {
					acc = regMap[regIdx]
				}

			case js.OpStar:
				regIdx := int(instr.OperandA)
				if regIdx < len(regMap) && acc != nil {
					regMap[regIdx] = acc
				}

			case js.OpAdd, js.OpSub, js.OpMul, js.OpDiv:
				rhsReg := int(instr.OperandA)
				rhs := acc // placeholder
				if rhsReg < len(regMap) && regMap[rhsReg] != nil {
					rhs = regMap[rhsReg]
				}
				if acc == nil || rhs == nil {
					continue
				}
				var op SSAOp
				switch instr.Op {
				case js.OpAdd:
					op = SSAAdd
				case js.OpSub:
					op = SSASub
				case js.OpMul:
					op = SSAMul
				case js.OpDiv:
					op = SSADiv
				}
				// Use integer arithmetic by default; type specialization may
				// upgrade to SSAFloat64 when IC feedback confirms numeric operands.
				node = g.newNode(op, SSAInt32, acc, rhs)
				node.BCPC = pc
				// Attach feedback slot if available.
				feedbackSlot := int(instr.OperandC)
				if bf.ICVector != nil && feedbackSlot < len(bf.ICVector.Slots) {
					node.Feedback = &bf.ICVector.Slots[feedbackSlot]
				}
				acc = node

			case js.OpNegate:
				if acc == nil {
					continue
				}
				node = g.newNode(SSANeg, SSAInt32, acc)
				node.BCPC = pc
				acc = node

			case js.OpToNumber:
				if acc == nil {
					continue
				}
				node = g.newNode(SSAToNumber, SSAInt32, acc)
				node.BCPC = pc
				acc = node

			case js.OpReturn:
				if acc != nil {
					node = g.newNode(SSAReturn, SSAAny, acc)
					node.BCPC = pc
				} else {
					node = g.newNode(SSAReturn, SSAAny)
					node.BCPC = pc
				}

			case js.OpCall, js.OpCall0, js.OpCall1, js.OpCall2:
				calleeReg := int(instr.OperandA)
				var callee *SSANode
				if calleeReg < len(regMap) && regMap[calleeReg] != nil {
					callee = regMap[calleeReg]
				}
				if callee == nil {
					continue
				}
				// Build arg list: [callee, arg0, arg1, ...] depending on call arity.
				args := []*SSANode{callee}
				switch instr.Op {
				case js.OpCall1:
					if acc != nil {
						args = append(args, acc)
					}
				case js.OpCall2:
					if acc != nil {
						args = append(args, acc)
					}
					arg2Reg := int(instr.OperandB)
					if arg2Reg < len(regMap) && regMap[arg2Reg] != nil {
						args = append(args, regMap[arg2Reg])
					}
				}
				node = g.newNode(SSACall, SSAAny, args...)
				node.BCPC = pc
				// Attach feedback for call site (OperandC).
				feedbackSlot := int(instr.OperandC)
				if bf.ICVector != nil && feedbackSlot < len(bf.ICVector.Slots) {
					node.Feedback = &bf.ICVector.Slots[feedbackSlot]
				}
				acc = node

			case js.OpJump, js.OpJumpIfFalse, js.OpJumpIfTrue,
				js.OpJumpIfToBooleanTrue, js.OpJumpIfToBooleanFalse,
				js.OpJumpIfNotNullish:
				// Control flow: create a Cmp node if conditional.
				if isConditionalBranch(instr.Op) && acc != nil {
					cmp := g.newNode(SSACmp, SSAAny, acc)
					cmp.BCPC = pc
					_ = cmp
				}
				// No value produced by branches.

			default:
				// Other opcodes: just track PC, produce no value.
				_ = fmt.Sprintf("unhandled opcode %v at pc %d", instr.Op, pc)
			}

			// Append node to the current basic block.
			if node != nil {
				bb.Nodes = append(bb.Nodes, node)
			}
		}
	}
	return nil
}

// buildBasicBlocks partitions bytecode into basic blocks based on leader identification.
//
// Leaders are:
//   - The first instruction (pc == 0)
//   - Any instruction that is the target of a branch/jump
//   - The instruction immediately after a branch/jump or return
func buildBasicBlocks(bf *js.BytecodeFunction) []*SSABasicBlock {
	leaders := make(map[int]bool)
	leaders[0] = true

	for pc, instr := range bf.Instructions {
		if isBranch(instr.Op) {
			target := int(instr.OperandA)
			if target < len(bf.Instructions) {
				leaders[target] = true
			}
			if pc+1 < len(bf.Instructions) {
				leaders[pc+1] = true
			}
		}
	}

	// Build blocks from leader-to-leader ranges.
	var blocks []*SSABasicBlock
	start := 0
	for pc := 1; pc <= len(bf.Instructions); pc++ {
		if leaders[pc] || pc == len(bf.Instructions) {
			bb := &SSABasicBlock{
				ID:      len(blocks),
				StartPC: start,
				EndPC:   pc - 1,
			}
			blocks = append(blocks, bb)
			start = pc
		}
	}

	// Link successor edges between blocks by examining the terminating instruction.
	for i, bb := range blocks {
		last := bf.Instructions[bb.EndPC]

		switch {
		case last.Op == js.OpJump:
			target := int(last.OperandA)
			if succ := blockForPC(blocks, target); succ != nil {
				bb.Successors = append(bb.Successors, succ)
			}

		case isConditionalBranch(last.Op):
			// Fall-through to next block + branch target.
			target := int(last.OperandA)
			if succ := blockForPC(blocks, target); succ != nil {
				bb.Successors = append(bb.Successors, succ)
			}
			if i+1 < len(blocks) {
				bb.Successors = append(bb.Successors, blocks[i+1])
			}

		case last.Op == js.OpReturn:
			// No successors (terminal).

		default:
			// Sequential fall-through.
			if i+1 < len(blocks) {
				bb.Successors = append(bb.Successors, blocks[i+1])
			}
		}
	}

	// Build predecessor edges from successor edges.
	for _, bb := range blocks {
		for _, succ := range bb.Successors {
			succ.Predecessors = append(succ.Predecessors, bb)
		}
	}

	return blocks
}

// isBranch returns true for bytecode operations that unconditionally or conditionally
// transfer control to an explicit target PC.
func isBranch(op js.Opcode) bool {
	switch op {
	case js.OpJump,
		js.OpJumpIfFalse, js.OpJumpIfTrue,
		js.OpJumpIfToBooleanTrue, js.OpJumpIfToBooleanFalse,
		js.OpJumpIfNotNullish,
		js.OpReturn:
		return true
	default:
		return false
	}
}

// isConditionalBranch returns true for branch operations that may fall through
// to the following instruction (i.e., have two successors).
func isConditionalBranch(op js.Opcode) bool {
	switch op {
	case js.OpJumpIfFalse, js.OpJumpIfTrue,
		js.OpJumpIfToBooleanTrue, js.OpJumpIfToBooleanFalse,
		js.OpJumpIfNotNullish:
		return true
	default:
		return false
	}
}

// blockForPC returns the basic block whose StartPC matches the given bytecode PC,
// or nil if no such block exists.
func blockForPC(blocks []*SSABasicBlock, pc int) *SSABasicBlock {
	for _, bb := range blocks {
		if bb.StartPC == pc {
			return bb
		}
	}
	return nil
}
