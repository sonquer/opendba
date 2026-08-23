package local

import (
	"strings"
	"testing"
)

func TestNeeded(t *testing.T) {
	entry := Entry{ID: "gemma-4-e4b-qat", Bytes: 4 << 30}

	allowed := Needed(entry, 8192)
	if allowed.Weights != entry.Bytes {
		t.Fatalf("weights = %d, want the measured size of the file", allowed.Weights)
	}
	if !allowed.Allowed {
		t.Fatal("a model that has not said how many layers it has must say the cache was allowed for")
	}
	if allowed.Total != allowed.Weights+allowed.Cache+allowed.Overhead {
		t.Fatalf("total = %d, want the parts added up", allowed.Total)
	}

	entry.Layers, entry.KVHeads, entry.HeadDim = 36, 8, 128
	worked := Needed(entry, 8192)
	if worked.Allowed {
		t.Fatal("a model that said what it is made of should have had its cache worked out")
	}
	if want := int64(2 * 36 * 8 * 128 * 2 * 8192); worked.Cache != want {
		t.Fatalf("cache = %d, want %d", worked.Cache, want)
	}
	if doubled := Needed(entry, 16384); doubled.Cache != worked.Cache*2 {
		t.Fatal("twice the context must cost twice the cache")
	}
}

func TestNeededDefaultsTheContext(t *testing.T) {
	entry := Entry{Bytes: 1 << 30, Layers: 36, KVHeads: 8, HeadDim: 128}
	if Needed(entry, 0).Cache != Needed(entry, DefaultContext).Cache {
		t.Fatal("a context nobody chose must fall back to the default")
	}
}

func TestCachePerToken(t *testing.T) {
	cases := map[string]Entry{
		"nothing known":  {},
		"no layers":      {KVHeads: 8, HeadDim: 128},
		"no heads":       {Layers: 36, HeadDim: 128},
		"no head width":  {Layers: 36, KVHeads: 8},
		"negative layer": {Layers: -1, KVHeads: 8, HeadDim: 128},
	}
	for name, entry := range cases {
		t.Run(name, func(t *testing.T) {
			if got := entry.cachePerToken(); got != 0 {
				t.Fatalf("cachePerToken() = %d, want nothing when the shape is unknown", got)
			}
		})
	}
}

func TestFits(t *testing.T) {
	entry := Entry{ID: "gemma-4-e4b-qat", Bytes: 4 << 30}
	cases := map[string]struct {
		machine     Machine
		fits        bool
		comfortable bool
		reason      string
	}{
		"plenty of everything": {
			machine:     Machine{Memory: 24 << 30, FreeDisk: 200 << 30},
			fits:        true,
			comfortable: true,
		},
		"the disk is what stops it": {
			machine: Machine{Memory: 64 << 30, FreeDisk: 5 << 30},
			reason:  "download needs",
		},
		"the memory is what stops it": {
			machine: Machine{Memory: 3 << 30, FreeDisk: 200 << 30},
			reason:  "running it needs",
		},
		"it fits but only just": {
			machine:     Machine{Memory: 6 << 30, FreeDisk: 200 << 30},
			fits:        true,
			comfortable: false,
			reason:      "leaves little",
		},
		"a machine that will not say": {
			machine:     Machine{},
			fits:        true,
			comfortable: true,
		},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			verdict := Fits(entry, 8192, test.machine)
			if verdict.Fits != test.fits {
				t.Fatalf("Fits = %v, want %v (%s)", verdict.Fits, test.fits, verdict.Reason)
			}
			if verdict.Comfortable != test.comfortable {
				t.Fatalf("Comfortable = %v, want %v (%s)", verdict.Comfortable, test.comfortable, verdict.Reason)
			}
			if test.reason == "" {
				if verdict.Reason != "" {
					t.Fatalf("Reason = %q, want none", verdict.Reason)
				}
				return
			}
			if !strings.Contains(verdict.Reason, test.reason) {
				t.Fatalf("Reason = %q, want it to mention %q", verdict.Reason, test.reason)
			}
		})
	}
}

func TestFitsLeavesRoomOnTheDisk(t *testing.T) {
	entry := Entry{Bytes: 4 << 30}
	tight := Fits(entry, 8192, Machine{Memory: 64 << 30, FreeDisk: 4<<30 + diskReserve - 1})
	if tight.Fits {
		t.Fatal("a download that would leave nothing on the disk was allowed")
	}
	enough := Fits(entry, 8192, Machine{Memory: 64 << 30, FreeDisk: 4<<30 + diskReserve + 1})
	if !enough.Fits {
		t.Fatalf("a download that leaves room was refused: %s", enough.Reason)
	}
}

func TestSize(t *testing.T) {
	cases := map[int64]string{
		0:          "0 B",
		512:        "512 B",
		4 << 10:    "4.0 KiB",
		20 << 20:   "20 MiB",
		4215695776: "3.9 GiB",
		1 << 40:    "1.0 TiB",
		1024 << 40: "1024 TiB",
		2620370976: "2.4 GiB",
	}
	for value, want := range cases {
		t.Run(want, func(t *testing.T) {
			if got := size(value); got != want {
				t.Fatalf("size(%d) = %q, want %q", value, got, want)
			}
		})
	}
}
