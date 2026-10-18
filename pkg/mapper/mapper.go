// Package mapper transforms values and collections with explicit mapping functions.
package mapper

// Func maps one input to an output.
type Func[In any, Out any] func(In) Out

// Map maps a non-nil input; nil returns the output zero value without calling fn.
func Map[In any, Out any](
	in *In,
	fn func(*In) Out,
) Out {
	var zero Out
	if in == nil {
		return zero
	}

	return fn(in)
}

// MapSlice preserves order and maps nil entries to output zero values.
func MapSlice[In any, Out any](
	items []*In,
	fn Func[*In, Out],
) []Out {
	result := make([]Out, len(items))
	var zero Out
	for i, item := range items {
		if item == nil {
			result[i] = zero
			continue
		}
		mapped := fn(item)
		result[i] = mapped
	}

	return result
}

// MapGroups maps each named group while preserving its key and item order.
func MapGroups[In any, Out any](
	m map[string][]*In,
	fn Func[*In, Out],
) map[string][]Out {
	result := make(map[string][]Out)
	for k, v := range m {
		result[k] = MapSlice(v, fn)
	}
	return result
}
