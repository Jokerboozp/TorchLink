package continuity

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
)

func identity(values ...any) string {
	b, _ := json.Marshal(values)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:12])
}
func clip(a, b Range) Range { return Range{max(a.Start, b.Start), min(a.End, b.End)} }
func valid(r Range) bool    { return r.Start < r.End }
func Union(ranges []Range, window Range) []Range {
	r := make([]Range, 0, len(ranges))
	for _, v := range ranges {
		if c := clip(v, window); valid(c) {
			r = append(r, c)
		}
	}
	slices.SortFunc(r, func(a, b Range) int {
		if a.Start < b.Start {
			return -1
		}
		if a.Start > b.Start {
			return 1
		}
		if a.End < b.End {
			return -1
		}
		if a.End > b.End {
			return 1
		}
		return 0
	})
	merged := make([]Range, 0, len(r))
	for _, v := range r {
		if len(merged) > 0 && v.Start <= merged[len(merged)-1].End {
			merged[len(merged)-1].End = max(merged[len(merged)-1].End, v.End)
		} else {
			merged = append(merged, v)
		}
	}
	return merged
}
func Duration(ranges []Range) int64 {
	var n int64
	for _, r := range ranges {
		n += r.End - r.Start
	}
	return n
}
func Intersection(a, b []Range, window Range) []Range {
	a = Union(a, window)
	b = Union(b, window)
	r := []Range{}
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		if c := clip(a[i], b[j]); valid(c) {
			r = append(r, c)
		}
		if a[i].End < b[j].End {
			i++
		} else {
			j++
		}
	}
	return r
}
func contains(ranges []Range, at int64) bool {
	i, found := slices.BinarySearchFunc(ranges, at, func(r Range, v int64) int {
		if r.End <= v {
			return -1
		}
		if r.Start > v {
			return 1
		}
		return 0
	})
	return found && i < len(ranges)
}
func endpoints(w Range, sets ...[]Range) []int64 {
	r := []int64{w.Start, w.End}
	for _, set := range sets {
		for _, v := range set {
			if c := clip(v, w); valid(c) {
				r = append(r, c.Start, c.End)
			}
		}
	}
	slices.Sort(r)
	return slices.Compact(r)
}
