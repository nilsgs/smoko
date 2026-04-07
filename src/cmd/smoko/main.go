package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

	cmd := &cobra.Command{
		Use:   "run <path>",
		Short: "Run .smoko test files",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTests(args[0], image, timeout, verbose, failFast)
		},
	}

	cmd.Flags().StringVar(&image, "image", "", "Docker image to use (overrides .smokorc and inline Image:)")
	cmd.Flags().IntVar(&timeout, "timeout", 0, "Seconds to wait for each When step (default: 30)")
	cmd.Flags().BoolVar(&verbose, "verbose", false, "Print stdout/stderr even for passing scenarios")
	cmd.Flags().BoolVar(&failFast, "fail-fast", false, "Stop after the first failed scenario")

	return cmd
}

func runTests(path, imageFlag string, timeoutFlag int, verbose, failFast bool) error {
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

	rep := reporter.New(os.Stdout, verbose)
	ctx := context.Background()
	allPassed := true

	for _, pf := range parsed {
		for _, feat := range pf.features {
			// Resolve image: flag > feature Image: > .smokorc
			img := resolveImage(imageFlag, feat.Image, cfg.Image)
			if img == "" {
				return fmt.Errorf("no Docker image specified for feature %q — use --image, Image: in .smoko file, or set image in .smokorc", feat.Name)
			}

			fmt.Fprintf(os.Stdout, "\nFeature: %s\n", feat.Name)

			// Pull image if not present
			if err := dc.PullIfMissing(ctx, img); err != nil {
				return fmt.Errorf("docker pull %s: %w", img, err)
			}

			for _, sc := range feat.Scenarios {
				result := runScenario(ctx, dc, feat, sc, img, timeout)
				result.FeatureName = feat.Name

				if !result.Passed || result.Error != nil {
					allPassed = false
				}
				rep.Add(result)

				if failFast && (!result.Passed || result.Error != nil) {
					rep.PrintSummary()
					if allPassed {
						return nil
					}
					return fmt.Errorf("tests failed")
				}
			}
		}
	}

	passed := rep.PrintSummary()
	if !passed || !allPassed {
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

	// Run Given steps
	for _, step := range allGiven {
		if step.ResolvedType != parser.StepGiven {
			continue
		}
		if err := executor.RunGiven(ctx, dc, containerID, step); err != nil {
			rep.Error = fmt.Errorf("Given %q: %w", step.Text, err)
			return rep
		}
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

	// Evaluate Then steps
	rep.Passed = true
	for _, step := range sc.Steps {
		if step.ResolvedType != parser.StepThen {
			continue
		}
		if whenResult == nil {
			ar := assertions.Result{Pass: false, Message: "no When step ran before this Then assertion"}
			rep.AssertionResults = append(rep.AssertionResults, ar)
			rep.Passed = false
			continue
		}
		ar := assertions.Evaluate(ctx, step, whenResult, dc, containerID)
		rep.AssertionResults = append(rep.AssertionResults, ar)
		if !ar.Pass {
			rep.Passed = false
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
