package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
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
	var noBuild bool

	var list bool

	cmd := &cobra.Command{
		Use:   "run [path]",
		Short: "Run .smoko test files",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := "specs"
			if len(args) > 0 {
				path = args[0]
			}
			if _, err := os.Stat(path); err != nil {
				if len(args) == 0 {
					return fmt.Errorf("no path given and no specs/ directory found — run 'smoko run <path>'")
				}
				return fmt.Errorf("path %q not found", path)
			}
			return runTests(path, image, timeout, cmd.Flags().Changed("timeout"), verbose, failFast, parallel, noBuild, list)
		},
	}

	cmd.Flags().StringVar(&image, "image", "", "Docker image to use (overrides .smokorc and inline Image:)")
	cmd.Flags().IntVar(&timeout, "timeout", config.DefaultTimeout, "Seconds to wait for each setup/action command")
	cmd.Flags().BoolVar(&verbose, "verbose", false, "Print stdout/stderr even for passing scenarios")
	cmd.Flags().BoolVar(&failFast, "fail-fast", false, "Stop after the first failed scenario")
	cmd.Flags().IntVar(&parallel, "parallel", 0, "Number of scenarios to run in parallel (0 = auto)")
	cmd.Flags().BoolVar(&noBuild, "no-build", false, "Skip the build step defined in .smokorc")
	cmd.Flags().BoolVar(&list, "list", false, "List scenarios without running them")

	return cmd
}

func runTests(path, imageFlag string, timeoutFlag int, timeoutFlagSet bool, verbose, failFast bool, parallel int, noBuild, list bool) error {
	// Determine working dir for config
	wd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("working directory: %w", err)
	}
	cfg, err := config.Load(wd)
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	// Run build command if configured
	if cfg.Build != "" && !noBuild {
		if err := runBuild(cfg.Build, wd); err != nil {
			return err
		}
	}

	// Resolve timeout
	timeout := resolveTimeout(cfg, timeoutFlag, timeoutFlagSet)

	// Resolve parallelism
	workers := resolveWorkerCount(parallel)

	// Collect .smoko files
	files, err := collectFiles(path)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("no .smoko files found at %s", path)
	}

	// Parse all files
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

	// --list: print scenarios and exit without running Docker
	if list {
		return listScenarios(parsed)
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

func resolveTimeout(cfg config.Config, timeoutFlag int, timeoutFlagSet bool) time.Duration {
	timeoutSec := config.DefaultTimeout
	if cfg.Timeout > 0 {
		timeoutSec = cfg.Timeout
	}
	if timeoutFlagSet && timeoutFlag > 0 {
		timeoutSec = timeoutFlag
	}
	return time.Duration(timeoutSec) * time.Second
}

func resolveWorkerCount(parallel int) int {
	if parallel <= 0 {
		return runtime.GOMAXPROCS(0)
	}
	return parallel
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

	// Run Given steps in declared order, batching adjacent file writes.
	workdir, givenEnv, err := executor.RunGivenSteps(ctx, dc, containerID, allGiven, timeout, env)
	if err != nil {
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
		wr, err := executor.RunWhen(ctx, dc, containerID, *whenStep, workdir, timeout)
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
		allResults := assertions.EvaluateAll(ctx, sc.Steps, whenResult, dc, containerID, givenEnv)
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

// runBuild executes the configured build command, streaming output to the
// terminal so users can see progress. dir is the .smokorc directory.
func runBuild(command, dir string) error {
	fmt.Fprintf(os.Stderr, "Building: %s\n", command)
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/C", command)
	} else {
		cmd = exec.Command("sh", "-c", command)
	}
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("build failed: %w", err)
	}
	return nil
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

type parsedFile struct {
	name     string
	features []parser.Feature
}

func listScenarios(files []parsedFile) error {
	totalFeatures := 0
	totalScenarios := 0
	for _, pf := range files {
		fmt.Printf("%s\n", pf.name)
		for _, f := range pf.features {
			totalFeatures++
			fmt.Printf("  Feature: %s\n", f.Name)
			for _, s := range f.Scenarios {
				totalScenarios++
				fmt.Printf("    · %s\n", s.Name)
			}
		}
		fmt.Println()
	}
	fmt.Printf("%d feature(s), %d scenario(s)\n", totalFeatures, totalScenarios)
	return nil
}
