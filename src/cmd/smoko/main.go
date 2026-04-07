package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/spf13/cobra"

	"github.com/nskut/smoko/internal/assertions"
	"github.com/nskut/smoko/internal/config"
	"github.com/nskut/smoko/internal/docker"
	"github.com/nskut/smoko/internal/executor"
	"github.com/nskut/smoko/internal/parser"
	"github.com/nskut/smoko/internal/reporter"
)

// Set via -ldflags at build time.
var (
	version = "dev"
	commit  = "none"
)

func main() {
	if err := rootCmd().Execute(); err != nil {
		os.Exit(2)
	}
}

func rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:          "smoko",
		Short:        "Platform-agnostic smoke testing tool for CLI applications",
		SilenceUsage: true,
	}
	root.Version = version + "+" + commit
	root.SetVersionTemplate("{{.Version}}\n")
	root.AddCommand(runCmd())
	return root
}

func runCmd() *cobra.Command {
	var image string
	var timeout int
	var verbose bool
	var failFast bool
	var parallel int

	cmd := &cobra.Command{
		Use:   "run <path>",
		Short: "Run .smoko test files",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTests(args[0], image, timeout, verbose, failFast, parallel)
		},
	}

	cmd.Flags().StringVar(&image, "image", "", "Docker image to use (overrides .smokorc and inline Image:)")
	cmd.Flags().IntVar(&timeout, "timeout", 0, "Seconds to wait for each When step (default: 30)")
	cmd.Flags().BoolVar(&verbose, "verbose", false, "Print stdout/stderr even for passing scenarios")
	cmd.Flags().BoolVar(&failFast, "fail-fast", false, "Stop after the first failed scenario")
	cmd.Flags().IntVar(&parallel, "parallel", 1, "Number of scenarios to run in parallel (0 = auto)")

	return cmd
}

func runTests(path, imageFlag string, timeoutFlag int, verbose, failFast bool, parallel int) error {
	// Determine working dir for config
	wd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("working directory: %w", err)
	}
	cfg, err := config.Load(wd)
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	// Resolve timeout
	timeoutSec := config.DefaultTimeout
	if cfg.Timeout > 0 {
		timeoutSec = cfg.Timeout
	}
	if timeoutFlag > 0 {
		timeoutSec = timeoutFlag
	}
	timeout := time.Duration(timeoutSec) * time.Second

	// Resolve parallelism
	workers := parallel
	if workers <= 0 {
		workers = runtime.GOMAXPROCS(0)
	}

	// Collect .smoko files
	files, err := collectFiles(path)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("no .smoko files found at %s", path)
	}

	// Parse all files
	type parsedFile struct {
		name     string
		features []parser.Feature
	}
	var parsed []parsedFile
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return fmt.Errorf("read %s: %w", f, err)
		}
		feats, err := parser.ParseFile(f, string(data))
		if err != nil {
			return fmt.Errorf("parse %s: %w", f, err)
		}
		parsed = append(parsed, parsedFile{name: f, features: feats})
	}

	// Docker client
	dc, err := docker.New()
	if err != nil {
		return fmt.Errorf("docker: %w", err)
	}
	defer dc.Close()

	// Build a flat list of scenario jobs with resolved images
	type scenarioJob struct {
		feat    parser.Feature
		sc      parser.Scenario
		img     string
		timeout time.Duration
	}

	var jobs []scenarioJob
	for _, pf := range parsed {
		for _, feat := range pf.features {
			img := resolveImage(imageFlag, feat.Image, cfg.Image)
			if img == "" {
				return fmt.Errorf("no Docker image specified for feature %q — use --image, Image: in .smoko file, or set image in .smokorc", feat.Name)
			}
			for _, sc := range feat.Scenarios {
				jobs = append(jobs, scenarioJob{feat: feat, sc: sc, img: img, timeout: timeout})
			}
		}
	}

	// Pull all unique images upfront (sequential — happens once)
	ctx := context.Background()
	seenImages := make(map[string]bool)
	for _, j := range jobs {
		if !seenImages[j.img] {
			seenImages[j.img] = true
			if err := dc.PullIfMissing(ctx, j.img); err != nil {
				return fmt.Errorf("docker pull %s: %w", j.img, err)
			}
		}
	}

	rep := reporter.New(os.Stdout, verbose)

	if workers == 1 {
		// Sequential path (original behavior, slightly optimized)
		allPassed := true
		currentFeature := ""
		for _, j := range jobs {
			if j.feat.Name != currentFeature {
				currentFeature = j.feat.Name
				fmt.Fprintf(os.Stdout, "\nFeature: %s\n", j.feat.Name)
			}
			result := runScenario(ctx, dc, j.feat, j.sc, j.img, j.timeout)
			result.FeatureName = j.feat.Name

			if !result.Passed || result.Error != nil {
				allPassed = false
			}
			rep.Add(result)

			if failFast && (!result.Passed || result.Error != nil) {
				rep.PrintSummary()
				return fmt.Errorf("tests failed")
			}
		}

		passed := rep.PrintSummary()
		if !passed || !allPassed {
			return fmt.Errorf("tests failed")
		}
		return nil
	}

	// Parallel path
	cancelCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var failed atomic.Bool
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup

	for _, j := range jobs {
		if failFast && failed.Load() {
			break
		}

		sem <- struct{}{} // acquire
		wg.Add(1)

		go func(j scenarioJob) {
			defer wg.Done()
			defer func() { <-sem }() // release

			if failFast && failed.Load() {
				return
			}

			result := runScenario(cancelCtx, dc, j.feat, j.sc, j.img, j.timeout)
			result.FeatureName = j.feat.Name

			if !result.Passed || result.Error != nil {
				failed.Store(true)
				if failFast {
					cancel()
				}
			}
			rep.Add(result)
		}(j)
	}

	wg.Wait()

	passed := rep.PrintSummary()
	if !passed || failed.Load() {
		return fmt.Errorf("tests failed")
	}
	return nil
}

func runScenario(ctx context.Context, dc *docker.Client, feat parser.Feature, sc parser.Scenario, img string, timeout time.Duration) reporter.ScenarioReport {
	rep := reporter.ScenarioReport{
		ScenarioName: sc.Name,
	}

	// Collect all steps: background + scenario (defensive copy to avoid aliasing feat.Background)
	allGiven := make([]parser.Step, len(feat.Background), len(feat.Background)+len(sc.Steps))
	copy(allGiven, feat.Background)
	allGiven = append(allGiven, sc.Steps...)
	env := executor.CollectEnvVars(allGiven)

	// Create container
	containerID, err := dc.CreateContainer(ctx, img, env)
	if err != nil {
		rep.Error = fmt.Errorf("create container: %w", err)
		return rep
	}
	defer dc.RemoveContainer(ctx, containerID)

	// Write all env vars to .smoko_env inside the container at once
	if err := executor.WriteEnvFile(ctx, dc, containerID, env); err != nil {
		rep.Error = fmt.Errorf("write env file: %w", err)
		return rep
	}

	// Run Given steps (batched for performance)
	if err := executor.RunGivenSteps(ctx, dc, containerID, allGiven); err != nil {
		rep.Error = err
		return rep
	}

	// Find and run the When step
	var whenStep *parser.Step
	for i := range sc.Steps {
		if sc.Steps[i].ResolvedType == parser.StepWhen {
			whenStep = &sc.Steps[i]
			break
		}
	}

	var whenResult *executor.WhenResult
	if whenStep != nil {
		wr, err := executor.RunWhen(ctx, dc, containerID, *whenStep, timeout)
		if err != nil {
			rep.Error = fmt.Errorf("When %q: %w", whenStep.Text, err)
			return rep
		}
		whenResult = wr
		rep.Stdout = wr.Stdout
		rep.Stderr = wr.Stderr
	}

	// Evaluate Then steps (batched filesystem checks for performance)
	rep.Passed = true
	thenSteps := make([]parser.Step, 0)
	thenIndices := make([]int, 0)
	for i, step := range sc.Steps {
		if step.ResolvedType == parser.StepThen {
			thenSteps = append(thenSteps, step)
			thenIndices = append(thenIndices, i)
		}
	}

	if whenResult == nil {
		for range thenSteps {
			ar := assertions.Result{Pass: false, Message: "no When step ran before this Then assertion"}
			rep.AssertionResults = append(rep.AssertionResults, ar)
			rep.Passed = false
		}
	} else {
		allResults := assertions.EvaluateAll(ctx, sc.Steps, whenResult, dc, containerID)
		for _, idx := range thenIndices {
			ar := allResults[idx]
			rep.AssertionResults = append(rep.AssertionResults, ar)
			if !ar.Pass {
				rep.Passed = false
			}
		}
	}

	return rep
}

func resolveImage(flagImg, featureImg, configImg string) string {
	if flagImg != "" {
		return flagImg
	}
	if featureImg != "" {
		return featureImg
	}
	return configImg
}

func collectFiles(path string) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", path, err)
	}
	if !info.IsDir() {
		return []string{path}, nil
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".smoko") || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		files = append(files, filepath.Join(path, e.Name()))
	}
	return files, nil
}
