// Package batch shares input discovery and bounded conversion scheduling between
// the command-line and desktop applications.
package batch

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ninghaofeng/pgs_converter/internal/convert"
)

type Job struct {
	Input, Output string
	Err           error
}

type Event struct {
	Job
	Index    int
	Started  bool
	Progress *convert.Progress
	Result   convert.Result
}

func Supported(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".srt", ".ass", ".ssa":
		return true
	}
	return false
}

// Discover preserves directory-relative paths and rejects every participant in
// an output collision. Directory symlinks are not traversed.
func Discover(inputs []string, output string, recursive bool) []Job {
	var jobs []Job
	for _, input := range inputs {
		info, err := os.Stat(input)
		if err != nil {
			jobs = append(jobs, Job{Input: input, Err: err})
			continue
		}
		if !info.IsDir() {
			j := Job{Input: input, Output: filepath.Join(output, strings.TrimSuffix(filepath.Base(input), filepath.Ext(input))+".sup")}
			if !Supported(input) {
				j.Err = fmt.Errorf("unsupported subtitle extension")
			}
			jobs = append(jobs, j)
			continue
		}
		err = filepath.WalkDir(input, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				jobs = append(jobs, Job{Input: path, Err: walkErr})
				if entry != nil && entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if entry.IsDir() {
				if path != input && !recursive {
					return filepath.SkipDir
				}
				return nil
			}
			if !Supported(path) {
				return nil
			}
			rel, err := filepath.Rel(input, path)
			if err != nil {
				return err
			}
			jobs = append(jobs, Job{Input: path, Output: filepath.Join(output, strings.TrimSuffix(rel, filepath.Ext(rel))+".sup")})
			return nil
		})
		if err != nil {
			jobs = append(jobs, Job{Input: input, Err: err})
		}
	}
	destinations := make(map[string][]int)
	for i, j := range jobs {
		if j.Err != nil {
			continue
		}
		abs, err := filepath.Abs(j.Output)
		if err != nil {
			jobs[i].Err = err
			continue
		}
		destinations[strings.ToLower(abs)] = append(destinations[strings.ToLower(abs)], i)
	}
	for _, indexes := range destinations {
		if len(indexes) > 1 {
			for _, i := range indexes {
				jobs[i].Err = fmt.Errorf("multiple inputs map to output %s", jobs[i].Output)
			}
		}
	}
	return jobs
}

// Run snapshots jobs/options and serializes worker events through one channel.
// Callers must drain the channel even after cancellation; undispatched jobs do
// not emit completion events. Each worker owns an independent renderer.
func Run(ctx context.Context, jobs []Job, opt convert.Options, workers int, progress bool) (<-chan Event, error) {
	if workers < 1 || workers > 32 {
		return nil, fmt.Errorf("jobs must be in 1..32")
	}
	jobs = append([]Job(nil), jobs...)
	opt.Fonts = append([]string(nil), opt.Fonts...)
	tasks := make(chan int)
	results := make(chan Event)
	var wg sync.WaitGroup
	for i := 0; i < min(workers, len(jobs)); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range tasks {
				j := jobs[index]
				var result convert.Result
				if j.Err == nil {
					if err := ctx.Err(); err != nil {
						j.Err = err
					} else {
						fileOpt := opt
						if progress {
							results <- Event{Job: j, Index: index, Started: true}
							fileOpt.Progress = func(p convert.Progress) {
								results <- Event{Job: j, Index: index, Progress: &p}
							}
						}
						result, j.Err = convert.File(ctx, j.Input, j.Output, fileOpt)
					}
				}
				results <- Event{Job: j, Index: index, Result: result}
			}
		}()
	}
	go func() {
		defer close(tasks)
		for i := range jobs {
			select {
			case tasks <- i:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() {
		wg.Wait()
		close(results)
	}()
	return results, nil
}
