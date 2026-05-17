package parser

import (
"strconv"
"strings"
)

// ParseCalc parses a CSS calc() expression and returns the computed value.
// This is a simplified evaluator supporting basic arithmetic.
func ParseCalc(expr string) (float64, error) {
expr = strings.TrimPrefix(expr, "calc(")
expr = strings.TrimSuffix(expr, ")")
expr = strings.TrimSpace(expr)
return evaluateCalcExpr(expr)
}

func evaluateCalcExpr(expr string) (float64, error) {
// Tokenize: split on operators while preserving them.
var tokens []string
current := strings.Builder{}
for i := 0; i < len(expr); i++ {
ch := expr[i]
if ch == '+' || ch == '-' || ch == '*' || ch == '/' {
if current.Len() > 0 {
tokens = append(tokens, strings.TrimSpace(current.String()))
current.Reset()
}
tokens = append(tokens, string(ch))
} else if ch == '(' || ch == ')' {
if current.Len() > 0 {
tokens = append(tokens, strings.TrimSpace(current.String()))
current.Reset()
}
tokens = append(tokens, string(ch))
} else {
current.WriteByte(ch)
}
}
if current.Len() > 0 {
tokens = append(tokens, strings.TrimSpace(current.String()))
}

// Convert to values and evaluate.
var values []float64
var ops []byte

for _, tok := range tokens {
switch tok {
case "+", "-", "*", "/":
ops = append(ops, tok[0])
default:
val := parseCSSValue(tok)
values = append(values, val)
}
}

if len(values) == 0 {
return 0, nil
}

// Evaluate * and / first.
for i := 0; i < len(ops); i++ {
if ops[i] == '*' || ops[i] == '/' {
if ops[i] == '*' {
values[i] = values[i] * values[i+1]
} else {
if values[i+1] != 0 {
values[i] = values[i] / values[i+1]
}
}
values = append(values[:i+1], values[i+2:]...)
ops = append(ops[:i], ops[i+1:]...)
i--
}
}

// Evaluate + and -.
result := values[0]
for i, op := range ops {
if op == '+' {
result += values[i+1]
} else if op == '-' {
result -= values[i+1]
}
}

return result, nil
}

func parseCSSValue(s string) float64 {
s = strings.TrimSpace(s)
// Strip units.
s = strings.TrimSuffix(s, "px")
s = strings.TrimSuffix(s, "em")
s = strings.TrimSuffix(s, "rem")
s = strings.TrimSuffix(s, "vw")
s = strings.TrimSuffix(s, "vh")
s = strings.TrimSuffix(s, "%")
s = strings.TrimSpace(s)
v, _ := strconv.ParseFloat(s, 64)
return v
}

// CSSVariables stores custom property (var()) values.
type CSSVariables map[string]string

// ParseCustomProperties extracts --custom-property declarations from CSS.
func ParseCustomProperties(cssText string) CSSVariables {
vars := make(CSSVariables)
// Match --name: value; patterns.
lines := strings.Split(cssText, ";")
for _, line := range lines {
line = strings.TrimSpace(line)
if !strings.HasPrefix(line, "--") {
continue
}
parts := strings.SplitN(line, ":", 2)
if len(parts) == 2 {
name := strings.TrimSpace(parts[0])
value := strings.TrimSpace(parts[1])
vars[name] = value
}
}
return vars
}

// ResolveVar resolves a var(--name) reference against the given variables.
func ResolveVar(value string, vars CSSVariables) string {
if !strings.HasPrefix(value, "var(") {
return value
}
inner := strings.TrimPrefix(value, "var(")
inner = strings.TrimSuffix(inner, ")")
// Check for fallback: var(--name, fallback)
parts := strings.SplitN(inner, ",", 2)
name := strings.TrimSpace(parts[0])
if v, ok := vars[name]; ok {
return v
}
if len(parts) > 1 {
return strings.TrimSpace(parts[1])
}
return ""
}

// KeyframesRule represents a @keyframes rule.
type KeyframesRule struct {
Name      string
Keyframes map[string]*KeyframeBlock
}

// KeyframeBlock represents a single keyframe stop.
type KeyframeBlock struct {
Selector     string // e.g., "0%", "from", "to", "50%"
Declarations []CSSDeclaration
}

// ParseKeyframes parses a @keyframes at-rule.
func ParseKeyframes(cssText string) *KeyframesRule {
rule := &KeyframesRule{
Keyframes: make(map[string]*KeyframeBlock),
}

// Extract name: @keyframes name { ... }
cssText = strings.TrimSpace(cssText)
cssText = strings.TrimPrefix(cssText, "@keyframes")
cssText = strings.TrimSpace(cssText)

// Find name (before {).
braceIdx := strings.Index(cssText, "{")
if braceIdx < 0 {
return rule
}
rule.Name = strings.TrimSpace(cssText[:braceIdx])

// Parse body (between { and }).
body := cssText[braceIdx+1:]
body = strings.TrimSuffix(body, "}")
body = strings.TrimSpace(body)

// Split on % or "from"/"to".
// Simple approach: find each keyframe block.
for {
body = strings.TrimSpace(body)
if body == "" {
break
}

// Find the selector (ends at {).
openIdx := strings.Index(body, "{")
if openIdx < 0 {
break
}
selector := strings.TrimSpace(body[:openIdx])
body = body[openIdx+1:]

// Find matching }.
closeIdx := strings.Index(body, "}")
if closeIdx < 0 {
break
}
blockCSS := body[:closeIdx]
body = body[closeIdx+1:]

// Parse declarations.
sheet := ParseCSS("*{" + blockCSS + "}")
var decls []CSSDeclaration
if len(sheet.Rules) > 0 {
for _, d := range sheet.Rules[0].Declarations { decls = append(decls, *d) }
}

rule.Keyframes[selector] = &KeyframeBlock{
Selector:     selector,
Declarations: decls,
}
}

return rule
}
