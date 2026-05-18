// vm_exceptions.go — Error/exception helper functions.
package js

func throwTypeErrorInFrame(frame *VMFrame, msg string) {
	frame.Thrown = NewString("TypeError: " + msg)
	frame.Acc = frame.Thrown
	frame.ShouldReturn = true
}

// throwReferenceErrorInFrame sets up a ReferenceError exception in the current frame.
func throwReferenceErrorInFrame(frame *VMFrame, msg string) {
	frame.Thrown = NewString("ReferenceError: " + msg)
	frame.Acc = frame.Thrown
	frame.ShouldReturn = true
}

// throwRangeErrorInFrame sets up a RangeError exception in the current frame.
func throwRangeErrorInFrame(frame *VMFrame, msg string) {
	frame.Thrown = NewString("RangeError: " + msg)
	frame.Acc = frame.Thrown
	frame.ShouldReturn = true
}
