package parser

import "testing"

func TestParseCalc(t *testing.T) {
v, err := ParseCalc("calc(10px + 20px)")
if err != nil {
t.Fatal(err)
}
if v != 30 {
t.Errorf("expected 30, got %v", v)
}

v2, _ := ParseCalc("calc(50px - 10px)")
if v2 != 40 {
t.Errorf("expected 40, got %v", v2)
}

v3, _ := ParseCalc("calc(10px * 3)")
if v3 != 30 {
t.Errorf("expected 30, got %v", v3)
}
}

func TestCustomProperties(t *testing.T) {
css := "--main-color: red; --font-size: 16px;"
vars := ParseCustomProperties(css)

if vars["--main-color"] != "red" {
t.Errorf("expected red, got %q", vars["--main-color"])
}
if vars["--font-size"] != "16px" {
t.Errorf("expected 16px, got %q", vars["--font-size"])
}
}

func TestResolveVar(t *testing.T) {
vars := CSSVariables{"--color": "blue", "--size": "12px"}

v := ResolveVar("var(--color)", vars)
if v != "blue" {
t.Errorf("expected blue, got %q", v)
}

v = ResolveVar("var(--unknown, red)", vars)
if v != "red" {
t.Errorf("expected fallback red, got %q", v)
}
}

func TestParseKeyframes(t *testing.T) {
css := `@keyframes fadeIn {
0% { opacity: 0; }
100% { opacity: 1; }
}`

kf := ParseKeyframes(css)
if kf.Name != "fadeIn" {
t.Errorf("expected fadeIn, got %q", kf.Name)
}
if len(kf.Keyframes) != 2 {
t.Errorf("expected 2 keyframes, got %d", len(kf.Keyframes))
}
if kf.Keyframes["0%"] == nil {
t.Error("missing 0% keyframe")
}
if kf.Keyframes["100%"] == nil {
t.Error("missing 100% keyframe")
}
}
