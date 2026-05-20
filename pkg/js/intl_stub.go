//go:build !intl

package js

// intl_stub.go — Stub Intl object when the intl build tag is not set.
// Keeps the module lightweight for go.dev/play and go get.
// Build with: go build -tags intl
func (vm *VM) registerIntl() {
	intlObj := NewJSObject()
	intlObj.ConstructorName = "Intl"
	vm.globals.M["Intl"] = NewObject(intlObj)
}
