package worldgen

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
)

// Limits on what the feature data may ask for (Task 4e fix round). The data is read from an install
// the owner points at, so a broken or hostile file must not crash or stall a generation worker: a
// number out of range makes its feature broken data (the rule is skipped and named in Problems), and
// a source chunk's decoration has a budget of scatter placements.

const (
	// maxDataNumber bounds every number of a feature file but weights; vanilla's largest is 236
	// (a carver's height limit), and heights, offsets and search spans stay within the world's height.
	maxDataNumber = 512
	// maxDataRadius bounds sizes across: radii and widths (vanilla's largest is 16).
	maxDataRadius = 64
	// maxDataWeight bounds a weight in a weighted list (vanilla's largest is 5000).
	maxDataWeight = 1 << 20
	// placementBudget is how many scatter placements one source chunk's decoration may make, rule and
	// nested scatters together. Vanilla's busiest chunk of the test seeds makes far fewer
	// (TestBedrockWorldsMatchBDS logs it and holds it under an eighth of the budget); past it, the rest of the source's scatters place nothing.
	placementBudget = 1 << 18
)

// checkDataNumbers walks a feature file and refuses numbers out of range. In a list of two whose
// second entry is a number and first is not, the number is a weight.
func checkDataNumbers(raw json.RawMessage) error {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return err
	}
	return checkNumbers(v, "")
}

func checkNumbers(v any, key string) error {
	switch t := v.(type) {
	case map[string]any:
		for k, e := range t {
			if err := checkNumbers(e, k); err != nil {
				return err
			}
		}
	case []any:
		if len(t) == 2 {
			if w, ok := t[1].(float64); ok {
				if _, num := t[0].(float64); !num {
					if w < 0 || w > maxDataWeight {
						return fmt.Errorf("weight %v in %s out of range", w, key)
					}
					return checkNumbers(t[0], key)
				}
			}
		}
		for _, e := range t {
			if err := checkNumbers(e, key); err != nil {
				return err
			}
		}
	case float64:
		limit := float64(maxDataNumber)
		if strings.Contains(key, "radius") || strings.Contains(key, "width") {
			limit = maxDataRadius
		}
		if t > limit || t < -limit || t != t {
			return fmt.Errorf("%s %v out of range (limit %v)", key, t, limit)
		}
	}
	return nil
}

// spend takes one placement from the source's budget, and says whether there was one.
func (d *decoration) spend() bool {
	if d.budget <= 0 {
		return false
	}
	d.budget--
	return true
}

// mostPlacements is the most scatter placements a source has made in this process, for the budget's
// test.
var mostPlacements atomic.Int64

func notePlacements(n int) {
	for {
		m := mostPlacements.Load()
		if int64(n) <= m || mostPlacements.CompareAndSwap(m, int64(n)) {
			return
		}
	}
}
