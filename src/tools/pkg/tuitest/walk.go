package tuitest

import (
	"runtime"
	"sort"
	"sync"
)

// Filter narrows a run down to one scenario, one size, or both.
type Filter struct {
	Scenario string
	Size     string
}

func (f Filter) wants(scenario Scenario, size Size) bool {
	if f.Scenario != "" && scenario.Name != f.Scenario {
		return false
	}
	return f.Size == "" || size.String() == f.Size
}

// Walk runs every scenario the filter allows, at every size it names, and hands
// back what each of them proved. Scenarios run beside each other because each
// one has a sandbox and a terminal of its own.
func Walk(runner Runner, filter Filter, jobs int) []Result {
	type work struct {
		scenario Scenario
		size     Size
	}
	var queue []work
	for _, scenario := range runner.Suite.Scenarios {
		for _, size := range runner.Suite.SizesFor(scenario) {
			if filter.wants(scenario, size) {
				queue = append(queue, work{scenario: scenario, size: size})
			}
		}
	}
	results := make([]Result, len(queue))
	limit := make(chan struct{}, Jobs(jobs))
	var group sync.WaitGroup
	for i, current := range queue {
		group.Add(1)
		go func() {
			defer group.Done()
			limit <- struct{}{}
			defer func() { <-limit }()
			result, err := runner.Run(current.scenario, current.size)
			result.Scenario, result.Size = current.scenario.Name, current.size
			if err != nil {
				result.Failures = append(result.Failures, Failure{Action: "run", Reason: err.Error()})
			}
			results[i] = result
		}()
	}
	group.Wait()
	sort.Slice(results, func(i, j int) bool {
		if results[i].Scenario != results[j].Scenario {
			return results[i].Scenario < results[j].Scenario
		}
		return results[i].Size.String() < results[j].Size.String()
	})
	return results
}

// Jobs is how many scenarios run at once, leaving a core for everything else.
func Jobs(asked int) int {
	if asked > 0 {
		return asked
	}
	return max(1, min(runtime.NumCPU()-1, 8))
}
