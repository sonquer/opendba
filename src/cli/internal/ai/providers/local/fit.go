package local

import "fmt"

const (
	// graphOverhead is what the compute graph, the shader library and the process
	// itself take before a single weight is loaded.
	graphOverhead = 700 << 20

	// cacheAllowance is what is set aside for the key and value cache when the
	// shape of the model is not known yet.
	cacheAllowance = 0.20

	// diskReserve is what is left on the disk after a download.
	diskReserve = 2 << 30

	// comfortable is the share of the memory budget a model may take before the
	// verdict stops being a plain yes.
	comfortable = 0.80
)

// Need is what running a model would take.
type Need struct {
	Weights  int64
	Cache    int64
	Overhead int64
	Total    int64

	// Allowed says the cache was set aside rather than worked out, which is the
	// case until the file has been opened and can be asked about its layers.
	Allowed bool
}

// Needed works out what a model would take at a given context length.
func Needed(entry Entry, context int) Need {
	if context <= 0 {
		context = DefaultContext
	}
	need := Need{Weights: entry.Bytes, Overhead: graphOverhead}
	if perToken := entry.cachePerToken(); perToken > 0 {
		need.Cache = perToken * int64(context)
	} else {
		need.Cache = int64(float64(entry.Bytes) * cacheAllowance)
		need.Allowed = true
	}
	need.Total = need.Weights + need.Cache + need.Overhead
	return need
}

// cachePerToken is what one token of context costs in the key and value cache.
func (e Entry) cachePerToken() int64 {
	if e.Layers <= 0 || e.KVHeads <= 0 || e.HeadDim <= 0 {
		return 0
	}
	const keyAndValue, bytesPerElement = 2, 2
	return int64(keyAndValue * e.Layers * e.KVHeads * e.HeadDim * bytesPerElement)
}

// Machine is what there is to run a model on.
type Machine struct {
	Memory   int64
	FreeDisk int64
}

// Verdict is whether a model fits, and what stops it when it does not.
type Verdict struct {
	Need        Need
	Fits        bool
	Comfortable bool
	Reason      string
}

// Fits weighs what a model needs against what a machine has.
func Fits(entry Entry, context int, machine Machine) Verdict {
	need := Needed(entry, context)
	verdict := Verdict{Need: need}
	switch {
	case machine.FreeDisk > 0 && entry.Bytes+diskReserve > machine.FreeDisk:
		verdict.Reason = fmt.Sprintf("the download needs %s and there is %s free, less what a machine has to keep",
			size(entry.Bytes), size(machine.FreeDisk))
	case machine.Memory > 0 && need.Total > machine.Memory:
		verdict.Reason = fmt.Sprintf("running it needs about %s and there is %s to run it in",
			size(need.Total), size(machine.Memory))
	default:
		verdict.Fits = true
		verdict.Comfortable = machine.Memory <= 0 || float64(need.Total) < comfortable*float64(machine.Memory)
		if !verdict.Comfortable {
			verdict.Reason = fmt.Sprintf("it would take about %s of the %s there is, which leaves little for anything else",
				size(need.Total), size(machine.Memory))
		}
	}
	return verdict
}

func size(value int64) string {
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	measure := float64(value)
	for _, unit := range units {
		if measure < 1024 || unit == "TiB" {
			if measure < 10 && unit != "B" {
				return fmt.Sprintf("%.1f %s", measure, unit)
			}
			return fmt.Sprintf("%.0f %s", measure, unit)
		}
		measure /= 1024
	}
	return fmt.Sprint(value)
}
