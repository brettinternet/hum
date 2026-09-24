package orchestrate

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"sync"
	"testing"
	"time"
)

func TestDownOrder(t *testing.T) {
	dependentFailure := errors.New("web stop failed")
	tests := []struct {
		name         string
		definitions  []Definition
		active       []string
		declared     []string
		waves        [][]string
		stopFailures map[string]error
	}{
		{
			name: "stack and independent records",
			definitions: []Definition{
				{Name: "db"},
				{Name: "api", After: []string{"db"}},
				{Name: "web", After: []string{"api"}},
				{Name: "worker"},
			},
			active:   []string{"db", "api", "web", "worker", "adhoc"},
			declared: []string{"db", "api", "web", "worker"},
			waves:    [][]string{{"adhoc", "web", "worker"}, {"api"}, {"db"}},
		},
		{
			name: "diamond waits for every dependent",
			definitions: []Definition{
				{Name: "db"},
				{Name: "api", After: []string{"db"}},
				{Name: "worker", After: []string{"db"}},
				{Name: "web", After: []string{"api", "worker"}},
			},
			active:   []string{"db", "api", "worker", "web"},
			declared: []string{"db", "api", "worker", "web"},
			waves:    [][]string{{"web"}, {"api", "worker"}, {"db"}},
		},
		{
			name: "inactive dependent does not delay prerequisite",
			definitions: []Definition{
				{Name: "db"},
				{Name: "api", After: []string{"db"}},
				{Name: "web", After: []string{"api"}},
				{Name: "worker"},
			},
			active:   []string{"db", "worker"},
			declared: []string{"db", "worker"},
			waves:    [][]string{{"db", "worker"}},
		},
		{
			name: "failed dependent releases prerequisite",
			definitions: []Definition{
				{Name: "db"},
				{Name: "api", After: []string{"db"}},
				{Name: "web", After: []string{"api"}},
			},
			active:       []string{"db", "api", "web"},
			declared:     []string{"db", "api", "web"},
			waves:        [][]string{{"web"}, {"api"}, {"db"}},
			stopFailures: map[string]error{"web": dependentFailure},
		},
		{
			name: "ad hoc record matching a declaration remains independent",
			definitions: []Definition{
				{Name: "db"},
				{Name: "api", After: []string{"db"}},
			},
			active:   []string{"db", "api"},
			declared: []string{"api"},
			waves:    [][]string{{"api", "db"}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			active := append([]string(nil), test.active...)
			sort.Strings(active)
			entered := make(chan string, len(active)+1)
			releases := make(map[string]chan struct{}, len(active))
			releaseOnce := make(map[string]*sync.Once, len(active))
			for _, name := range active {
				releases[name] = make(chan struct{})
				releaseOnce[name] = &sync.Once{}
			}
			release := func(name string) {
				if once := releaseOnce[name]; once != nil {
					once.Do(func() { close(releases[name]) })
				}
			}
			releaseAll := func() {
				for _, name := range active {
					release(name)
				}
			}

			type stopEvent struct {
				name string
				kind string
			}
			var eventsMu sync.Mutex
			var events []stopEvent
			record := func(name, kind string) {
				eventsMu.Lock()
				events = append(events, stopEvent{name: name, kind: kind})
				eventsMu.Unlock()
			}

			done := make(chan map[string]error, 1)
			go func() {
				done <- OrchestrateDown(context.Background(), test.definitions, test.active, test.declared, func(_ context.Context, name string) error {
					entered <- name
					record(name, "start")
					<-releases[name]
					record(name, "finish")
					return test.stopFailures[name]
				})
			}()

			for _, wantWave := range test.waves {
				gotWave := make([]string, 0, len(wantWave))
				for len(gotWave) < len(wantWave) {
					select {
					case name := <-entered:
						gotWave = append(gotWave, name)
					case <-time.After(2 * time.Second):
						releaseAll()
						t.Fatalf("stop wave stalled: got %v, waiting for %v", gotWave, wantWave)
					}
				}
				sort.Strings(gotWave)
				want := append([]string(nil), wantWave...)
				sort.Strings(want)
				if !reflect.DeepEqual(gotWave, want) {
					releaseAll()
					<-done
					t.Fatalf("stop wave = %v, want %v", gotWave, want)
				}
				for _, name := range wantWave {
					release(name)
				}
			}

			var results map[string]error
			select {
			case results = <-done:
			case <-time.After(2 * time.Second):
				releaseAll()
				t.Fatal("down orchestration did not complete")
			}
			for _, name := range active {
				wantErr := test.stopFailures[name]
				if gotErr, ok := results[name]; !ok || !errors.Is(gotErr, wantErr) {
					t.Errorf("result for %q = (%v, present %t), want (%v, present true)", name, gotErr, ok, wantErr)
				}
			}
			if len(results) != len(active) {
				t.Errorf("stop results = %v, want only active names %v", results, active)
			}

			eventsMu.Lock()
			defer eventsMu.Unlock()
			positions := make(map[string]map[string]int, len(active))
			for index, event := range events {
				if positions[event.name] == nil {
					positions[event.name] = make(map[string]int)
				}
				positions[event.name][event.kind] = index
			}
			for waveIndex := 1; waveIndex < len(test.waves); waveIndex++ {
				lastPreviousFinish := -1
				for _, name := range test.waves[waveIndex-1] {
					if position := positions[name]["finish"]; position > lastPreviousFinish {
						lastPreviousFinish = position
					}
				}
				for _, name := range test.waves[waveIndex] {
					if start := positions[name]["start"]; start <= lastPreviousFinish {
						t.Errorf("%q started at %d before prior wave completed at %d; events=%v", name, start, lastPreviousFinish, events)
					}
				}
			}
		})
	}
}
