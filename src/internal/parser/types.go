package parser

// StepType represents the keyword that introduces a step.
type StepType int

const (
	StepGiven StepType = iota
	StepWhen
	StepThen
	StepAnd
	StepBut
)

// Step is a single Given/When/Then/And/But line in a scenario.
type Step struct {
	Type       StepType
	// ResolvedType is the effective type after And/But inheritance (never And/But).
	ResolvedType StepType
	Text         string // trimmed text after the keyword
	Block        string // multi-line body (may be empty)
	Line         int    // source line number (1-based)
}

// Scenario is a single test case.
type Scenario struct {
	Name  string
	Steps []Step
	Line  int
}

// Feature is the top-level container parsed from a .smoko file.
type Feature struct {
	Name        string
	Description string
	Image       string     // optional inline image declaration
	Background  []Step     // steps prepended to every scenario
	Scenarios   []Scenario
}
