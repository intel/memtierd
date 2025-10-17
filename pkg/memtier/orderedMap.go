package memtier

import (
	rbt "github.com/emirpasic/gods/trees/redblacktree"
)

// OrderedMap represents an ordered map data structure.
// It combines the functionalities of a Red-Black Tree and a key-value map.
// The keys are uint64 values, and the values are of any type specified by the user.
type OrderedMap[V any] struct {
	rbt.Tree // Tree is the underlying Red-Black Tree used for maintaining the order of keys.
}

func compareUint64(a, b interface{}) int {
	if a.(uint64) < b.(uint64) {
		return -1
	} else if a.(uint64) > b.(uint64) {
		return 1
	}
	return 0
}

// NewOrderedMap creates and returns a new instance of OrderedMap with the specified value type.
func NewOrderedMap[V any]() *OrderedMap[V] {
	return &OrderedMap[V]{
		Tree: rbt.Tree{
			// Set the Comparator function to compareUint64 for sorting keys.
			Comparator: compareUint64,
		},
	}
}

// Get returns the value for a key.
// If the key does not exist, returns nil or default value and false.
func (m *OrderedMap[V]) Get(key uint64) (value V, ok bool) {
	v, ok := m.Ceiling(key)
	if ok {
		value = v.Value.(V)
	}
	return value, ok
}

// GetElement returns the value for a key.
// If the key does not exist, returns nil.
func (m *OrderedMap[V]) GetElement(key uint64) (value V) {
	v, ok := m.Ceiling(key)
	if ok {
		value = v.Value.(V)
	}
	return value
}

// Set will set (or replace) a value for a key.
// If the key already exists, it updates the value and returns false.
// If the key is new, it adds the key-value pair and returns true.
func (m *OrderedMap[V]) Set(key uint64, value V) bool {
	_, ok := m.Ceiling(key)
	m.Put(key, value)
	return !ok
}

// Len returns the number of elements in the OrderedMap.
func (m *OrderedMap[V]) Len() int {
	return m.Size()
}

// FindRange returns a slice of values within the range of [start, end] in the OrderedMap.
// It includes values whose keys are greater than or equal to start and less than or equal to end.
func (m *OrderedMap[V]) FindRange(start, end uint64) (collections []V) {
	// Find the nearest nodes for the start and end keys.
	s, _ := m.Floor(start)
	if s == nil {
		s = m.Left()
	}
	e, _ := m.Floor(end)
	if e == nil {
		e = m.Right()
	}

	// If any of the nodes are nil or the keys are not within the range, return an empty collection.
	if s == nil || e == nil {
		return collections
	}

	// Iterate over the nodes between 's' and 'e' and collect their values.
	iter := m.IteratorAt(s)
	for {
		if m.Comparator(iter.Key(), end) > 0 {
			break
		}
		collections = append(collections, iter.Value().(V))
		if !iter.Next() {
			break
		}
	}
	return collections
}

// Delete removes the key-value pair.
func (m *OrderedMap[V]) Delete(key uint64) {
	m.Remove(key)
}

// Foreach iterates over each key-value pair and applies the given function to the values.
// The iteration stops if the function returns false.
func (m *OrderedMap[V]) Foreach(f func(V) int) {
	walker := m.Iterator()
	for walker.Next() {
		if f(walker.Node().Value.(V)) != 0 {
			break
		}
	}
}
