package js_test

import (
"testing"
js "github.com/lucasdss/v8go/pkg/js"
)

func TestProxyGetTrapInlineFinal(t *testing.T) {
vm := js.NewVM()
result := vm.Run(`var p=new Proxy({x:1},{get:function(t,k){return t[k]*2}}); p.x`)
if result.ToNumber() != 2 {
t.Errorf("proxy get trap: expected 2, got %v", result.ToNumber())
}
}

func TestSetFromArrayHas(t *testing.T) {
vm := js.NewVM()
result := vm.Run(`new Set([1,2,3]).has(2)`)
if !result.IsTruthy() {
t.Error("Set from array: has(2) should be true")
}
}

func TestSetFromArraySize(t *testing.T) {
vm := js.NewVM()
result := vm.Run(`new Set([1,2,3]).size`)
if result.ToNumber() != 3 {
t.Errorf("Set from array size: expected 3, got %v", result.ToNumber())
}
}
