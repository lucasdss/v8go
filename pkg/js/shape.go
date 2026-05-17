// shape.go — Hidden Classes (Shapes): V8's secret sauce for fast property access.
//
// In V8, every JS object has a "Map" (Hidden Class) that describes its layout.
// Objects with the same properties in the same order share the same Shape,
// enabling fixed-offset property reads without hash-table lookups.
//
// V8 alignment: each property carries attributes (writable, enumerable, configurable)
// per src/objects/property-details.h.
package js

import (
	"strings"
	"sync"
)

// PropertyAttr encodes V8-style property attributes.
type PropertyAttr uint8

const (
	AttrWritable     PropertyAttr = 1 << 0 // READ_ONLY inverse: can be written
	AttrEnumerable   PropertyAttr = 1 << 1 // DONT_ENUM inverse: visible in for-in
	AttrConfigurable PropertyAttr = 1 << 2 // DONT_DELETE inverse: can be deleted/reconfigured

	// Default attributes for regular properties (all true).
	AttrDefault = AttrWritable | AttrEnumerable | AttrConfigurable
)

// PropEntry describes a single property in a Shape.
type PropEntry struct {
	Offset int
	Attr   PropertyAttr
}

// Shape represents a Hidden Class describing property layout.
// Shapes form a transition tree: adding property "x" to an empty Shape
// creates a new child Shape with "x" at offset 0.
type Shape struct {
	// Properties maps property name → slot index + attributes.
	Properties map[string]PropEntry
	// Transitions maps property name → child Shape for adding that property.
	Transitions map[string]*Shape
	// Parent is the Shape this one transitioned from (nil for root/empty).
	Parent *Shape
	// PropertyCount is the number of own properties this Shape describes.
	PropertyCount int
	// IsDictionary is true when properties are stored in a hash map (slow path)
	// rather than inline slots (fast path).
	IsDictionary bool
	// SlackCounter tracks remaining constructions before shape size finalizes.
	// V8 uses this to over-allocate property slots for the first N objects,
	// avoiding repeated reallocations when objects grow differently. Default: 7.
	SlackCounter   int
	SlackFinal     bool // true when shape size is finalized
	FinalPropCount int  // property count after finalization
}

// shapeCache stores pre-computed Shapes keyed by comma-joined property names.
// Eliminates repeated Shape transition chains for common object literal patterns.
var shapeCache sync.Map

// GetOrCreateShape returns a cached Shape for the given property-name sequence,
// creating and caching it on first use. This avoids building the transition chain
// (EmptyShape → AddField("x") → AddField("y")) for every object literal.
func GetOrCreateShape(propNames []string) *Shape {
	key := strings.Join(propNames, ",")
	if s, ok := shapeCache.Load(key); ok {
		return s.(*Shape)
	}
	shape := EmptyShape
	for _, name := range propNames {
		shape = shape.AddProperty(name)
	}
	shapeCache.Store(key, shape)
	return shape
}

// EmptyShape is the root Shape for objects with no own properties.
var EmptyShape = &Shape{
	Properties:    make(map[string]PropEntry),
	Transitions:   make(map[string]*Shape),
	PropertyCount: 0,
	SlackCounter:  7,
}

// AddProperty returns a Shape for an object with the named property added.
func (s *Shape) AddProperty(name string) *Shape {
	return s.AddPropertyWithAttr(name, AttrDefault)
}

// AddPropertyWithAttr returns a Shape for the named property with given attributes.
func (s *Shape) AddPropertyWithAttr(name string, attr PropertyAttr) *Shape {
	if child, ok := s.Transitions[name]; ok {
		return child
	}
	child := &Shape{
		Properties:    make(map[string]PropEntry),
		Transitions:   make(map[string]*Shape),
		Parent:        s,
		PropertyCount: s.PropertyCount + 1,
		SlackCounter:  s.SlackCounter,
		SlackFinal:    s.SlackFinal,
	}
	for k, v := range s.Properties {
		child.Properties[k] = v
	}
	offset := s.PropertyCount
	child.Properties[name] = PropEntry{Offset: offset, Attr: attr}
	s.Transitions[name] = child
	return child
}

// GetOffset returns the slot index for a property, or -1 if not found.
func (s *Shape) GetOffset(name string) int {
	if entry, ok := s.Properties[name]; ok {
		return entry.Offset
	}
	return -1
}

// GetAttr returns the property attributes, or 0 if not found.
func (s *Shape) GetAttr(name string) PropertyAttr {
	if entry, ok := s.Properties[name]; ok {
		return entry.Attr
	}
	return 0
}

// HasProperty returns true if this Shape describes the named property.
func (s *Shape) HasProperty(name string) bool {
	_, ok := s.Properties[name]
	return ok
}

// ConvertToDictionary transitions this Shape to dictionary mode.
func (s *Shape) ConvertToDictionary() *Shape {
	dict := &Shape{
		Properties:    make(map[string]PropEntry),
		Transitions:   make(map[string]*Shape),
		Parent:        s,
		PropertyCount: s.PropertyCount,
		IsDictionary:  true,
	}
	for k, v := range s.Properties {
		dict.Properties[k] = v
	}
	return dict
}
