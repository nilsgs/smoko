package main

import (
	"bytes"
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
	var output string
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
					return fmt.Errorf("no path given and no specs/ directory found - run 'smoko run <path>'")
				}
				return fmt.Errorf("path %q not found", path)
			}
			return runTests(path, image, timeout, cmd.Flags().Changed("timeout"), verbose, output, failFast, parallel, noBuild, list)
		},
	}

	cmd.Flags().StringVar(&image, "image", "", "Docker image to use (overrides .smokorc and inline Image:)")
	cmd.Flags().IntVar(&timeout, "timeout", config.DefaultTimeout, "Seconds to wait for each setup/action command")
	cmd.Flags().BoolVar(&verbose, "verbose", false, "Print stdout/stderr in the final report, including passing scenarios")
	cmd.Flags().StringVar(&output, "output", "", "Machine-readable output format (supported: json)")
	cmd.Flags().BoolVar(&failFast, "fail-fast", false, "Stop after the first failed scenario")
	cmd.Flags().IntVar(&parallel, "parallel", 0, "Number of scenarios to run in parallel (0 = auto)")
	cmd.Flags().BoolVar(&noBuild, "no-build", false, "Skip the build step defined in .smokorc")
	cmd.Flags().BoolVar(&list, "list", false, "List scenarios without running them")

	return cmd
}

func runTests(path, imageFlag string, timeoutFlag int, timeoutFlagSet bool, verbose bool, output string, failFast bool, parallel int, noBuild, list bool) error {
	suiteStart := time.Now()

	outputMode, err := parseOutputMode(output)
	if err != nil {
		return err
	}

	rep := reporter.New(os.Stdout, verbose, outputMode, version+"+"+commit)

	wd, err := os.Getwd()
	if err != nil {
		return emitFatal(rep, outputMode, suiteStart, fmt.Errorf("working directory: %w", err))
	}
	cfg, err := config.Load(wd)
	if err != nil {
		return emitFatal(rep, outputMode, suiteStart, fmt.Errorf("config: %w", err))
	}

	if cfg.Build != "" && !noBuild {
		if err := runBuild(cfg.Build, wd, outputMode, verbose); err != nil {
			return emitFatal(rep, outputMode, suiteStart, err)
		}
	}

	timeout := resolveTimeout(cfg, timeoutFlag, timeoutFlagSet)
	workers := resolveWorkerCount(parallel)

	files, err := collectFiles(path)
	if err != nil {
		return emitFatal(rep, outputMode, suiteStart, err)
	}
	if len(files) == 0 {
		return emitFatal(rep, outputMode, suiteStart, fmt.Errorf("no .smoko files found at %s", path))
	}

	var parsed []parsedFile
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return emitFatal(rep, outputMode, suiteStart, fmt.Errorf("read %s: %w", f, err))
		}
		feats, err := parser.ParseFile(f, string(data))
		if err != nil {
			return emitFatal(rep, outputMode, suiteStart, fmt.Errorf("parse %s: %w", f, err))
		}
		parsed = append(parsed, parsedFile{name: f, features: feats})
	}
	if err := validateParsedFiles(parsed); err != nil {
		return emitFatal(rep, outputMode, suiteStart, err)
	}

	if list {
		return listScenarios(parsed)
	}

	dc, err := docker.New()
	if err != nil {
		return emitFatal(rep, outputMode, suiteStart, fmt.Errorf("docker: %w", err))
	}
	defer dc.Close()

	type scenarioJob struct {
		order   int
		file    string
		feat    parser.Feature
		sc      parser.Scenario
		img     string
		timeout time.Duration
	}

	var jobs []scenarioJob
	order := 0
	for _, pf := range parsed {
		for _, feat := range pf.features {
			img := resolveImage(imageFlag, feat.Image, cfg.Image)
			if img == "" {
				return emitFatal(rep, outputMode, suiteStart, fmt.Errorf("no Docker image specified for feature %q - use --image, Image: in .smoko file, or set image in .smokorc", feat.Name))
			}
			for _, sc := range feat.Scenarios {
				jobs = append(jobs, scenarioJob{
					order:   order,
					file:    pf.name,
					feat:    feat,
					sc:      sc,
					img:     img,
					timeout: timeout,
				})
				order++
			}
		}
	}

	ctx := context.Background()
	seenImages := make(map[string]bool)
	for _, j := range jobs {
		if seenImages[j.img] {
			continue
		}
		seenImages[j.img] = true
		if err := dc.PullIfMissing(ctx, j.img); err != nil {
			return emitFatal(rep, outputMode, suiteStart, fmt.Errorf("docker pull %s: %w", j.img, err))
		}
	}

	if workers == 1 {
		allPassed := true
		for _, j := range jobs {
			result := runScenario(ctx, dc, j.file, j.order, j.feat, j.sc, j.img, j.timeout)
			if !result.Passed || result.Error != nil {
				allPassed = false
			}
			rep.Add(result)

			if failFast && (!result.Passed || result.Error != nil) {
				rep.PrintSummary(time.Since(suiteStart), true)
				return fmt.Errorf("tests failed")
			}
		}

		passed := rep.PrintSummary(time.Since(suiteStart), false)
		if !passed || !allPassed {
			return fmt.Errorf("tests failed")
		}
		return nil
	}

	cancelCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var failed atomic.Bool
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup

	for _, j := range jobs {
		if failFast && failed.Load() {
			break
		}

		sem <- struct{}{}
		wg.Add(1)

		go func(job scenarioJob) {
			defer wg.Done()
			defer func() { <-sem }()

			if failFast && failed.Load() {
				return
			}

			result := runScenario(cancelCtx, dc, job.file, job.order, job.feat, job.sc, job.img, job.timeout)
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

	passed := rep.PrintSummary(time.Since(suiteStart), failFast && failed.Load())
	if !passed || failed.Load() {
		return fmt.Errorf("tests failed")
	}
	return nil
}

func emitFatal(rep *reporter.Reporter, mode reporter.OutputMode, suiteStart time.Time, err error) error {
	if mode == reporter.OutputModeJSON {
		rep.PrintFatal(err, time.Since(suiteStart))
	}
	return err
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

func runScenario(ctx context.Context, dc *docker.Client, file string, order int, feat parser.Feature, sc parser.Scenario, img string, timeout time.Duration) (rep reporter.ScenarioReport) {
	start := time.Now()
	rep = reporter.ScenarioReport{
		Order:        order,
		File:         file,
		FeatureName:  feat.Name,
		ScenarioName: sc.Name,
		ScenarioLine: sc.Line,
	}
	allGiven := make([]parser.Step, len(feat.Background), len(feat.Background)+len(sc.Steps))
	copy(allGiven, feat.Background)
	allGiven = append(allGiven, sc.Steps...)
	env := executor.CollectEnvVars(allGiven)

	containerID, err := dc.CreateContainer(ctx, img, env)
	if err != nil {
		rep.Error = fmt.Errorf("create container: %w", err)
		rep.Duration = time.Since(start)
		return rep
	}
	defer dc.RemoveContainer(ctx, containerID)
	// Registered after RemoveContainer so it runs first (LIFO), excluding teardown from duration.
	defer func() { rep.Duration = time.Since(start) }()

	if err := executor.WriteEnvFile(ctx, dc, containerID, env); err != nil {
		rep.Error = fmt.Errorf("write env file: %w", err)
		return rep
	}

	workdir, givenEnv, err := executor.RunGivenSteps(ctx, dc, containerID, allGiven, timeout, env)
	if err != nil {
		rep.Error = err
		return rep
	}

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
		rep.ExitCode = &wr.ExitCode
	}

	rep.Passed = true
	thenIndices := make([]int, 0)
	for i, step := range sc.Steps {
		if step.ResolvedType == parser.StepThen {
			thenIndices = append(thenIndices, i)
		}
	}

	if whenResult == nil {
		for _, idx := range thenIndices {
			step := sc.Steps[idx]
			rep.AssertionResults = append(rep.AssertionResults, reporter.AssertionReport{
				Pass:     false,
				Message:  "no When step ran before this Then assertion",
				StepText: step.Text,
				StepLine: step.Line,
			})
			rep.Passed = false
		}
		return
	}

	allResults := assertions.EvaluateAll(ctx, sc.Steps, whenResult, dc, containerID, givenEnv)
	for _, idx := range thenIndices {
		ar := allResults[idx]
		step := sc.Steps[idx]
		rep.AssertionResults = append(rep.AssertionResults, reporter.AssertionReport{
			Pass:     ar.Pass,
			Message:  ar.Message,
			StepText: step.Text,
			StepLine: step.Line,
		})
		if !ar.Pass {
			rep.Passed = false
		}
	}

	return
}

func parseOutputMode(output string) (reporter.OutputMode, error) {
	switch output {
	case "":
		return reporter.OutputModeText, nil
	case string(reporter.OutputModeJSON):
		return reporter.OutputModeJSON, nil
	default:
		return "", fmt.Errorf("unsupported output format %q (supported: json)", output)
	}
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

func runBuild(command, dir string, outputMode reporter.OutputMode, verbose bool) error {
	fmt.Fprintf(os.Stderr, "Building: %s\n", command)
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/C", command)
	} else {
		cmd = exec.Command("sh", "-c", command)
	}
	cmd.Dir = dir
	if verbose {
		if outputMode == reporter.OutputModeJSON {
			cmd.Stdout = os.Stderr
		} else {
			cmd.Stdout = os.Stdout
		}
		cmd.Stderr = os.Stderr
	} else {
		var buf bytes.Buffer
		cmd.Stdout = &buf
		cmd.Stderr = &buf
		if err := cmd.Run(); err != nil {
			fmt.Fprint(os.Stderr, buf.String())
			return fmt.Errorf("build failed: %w", err)
		}
		return nil
	}
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

func validateParsedFiles(files []parsedFile) error {
	for _, pf := range files {
		for _, feat := range pf.features {
			if err := validateBackground(pf.name, feat); err != nil {
				return err
			}
			for _, sc := range feat.Scenarios {
				if err := validateScenario(pf.name, feat.Name, sc); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func validateBackground(file string, feat parser.Feature) error {
	for _, step := range feat.Background {
		if step.ResolvedType != parser.StepGiven {
			return fmt.Errorf("%s:%d: feature %q background step %q must be a Given step", file, step.Line, feat.Name, step.Text)
		}
	}
	return nil
}

func validateScenario(file, featureName string, sc parser.Scenario) error {
	whenCount := 0
	thenCount := 0
	phase := parser.StepGiven

	for _, step := range sc.Steps {
		switch step.ResolvedType {
		case parser.StepGiven:
			if phase != parser.StepGiven {
				return fmt.Errorf("%s:%d: scenario %q has Given step %q after the When step", file, step.Line, sc.Name, step.Text)
			}
		case parser.StepWhen:
			whenCount++
			if whenCount > 1 {
				return fmt.Errorf("%s:%d: scenario %q has multiple When steps", file, step.Line, sc.Name)
			}
			if phase == parser.StepThen {
				return fmt.Errorf("%s:%d: scenario %q has When step %q after Then assertions", file, step.Line, sc.Name, step.Text)
			}
			phase = parser.StepWhen
		case parser.StepThen:
			if whenCount == 0 {
				return fmt.Errorf("%s:%d: scenario %q has Then step %q before a When step", file, step.Line, sc.Name, step.Text)
			}
			thenCount++
			phase = parser.StepThen
		default:
			return fmt.Errorf("%s:%d: scenario %q has unsupported step type for %q", file, step.Line, sc.Name, step.Text)
		}
	}

	if whenCount == 0 {
		return fmt.Errorf("%s:%d: feature %q scenario %q must contain exactly one When step", file, sc.Line, featureName, sc.Name)
	}
	if thenCount == 0 {
		return fmt.Errorf("%s:%d: feature %q scenario %q must contain at least one Then assertion", file, sc.Line, featureName, sc.Name)
	}
	return nil
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
				fmt.Printf("    - %s\n", s.Name)
			}
		}
		fmt.Println()
	}
	fmt.Printf("%d feature(s), %d scenario(s)\n", totalFeatures, totalScenarios)
	return nil
}
