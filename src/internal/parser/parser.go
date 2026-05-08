package parser

import (
	"fmt"
	"strings"
)

// ParseFile parses the content of a .smoko file and returns the features it defines.
// filename is used only for error messages.
func ParseFile(filename, content string) ([]Feature, error) {
	lines := strings.Split(content, "\n")
	tokens := Tokenize(lines)
	return parse(filename, tokens)
}

func parse(filename string, tokens []Token) ([]Feature, error) {
	p := &parser{filename: filename, tokens: tokens}
	return p.parseAll()
}

type parser struct {
	filename string
	tokens   []Token
	pos      int
}

func (p *parser) peek() *Token {
	for p.pos < len(p.tokens) {
		t := &p.tokens[p.pos]
		if t.Kind == TokBlank || t.Kind == TokComment {
			p.pos++
			continue
		}
		return t
	}
	return nil
}

func (p *parser) next() *Token {
	t := p.peek()
	if t != nil {
		p.pos++
	}
	return t
}

func (p *parser) parseAll() ([]Feature, error) {
	var features []Feature
	for {
		tags, tagLine, err := p.parseTags()
		if err != nil {
			return nil, err
		}
		t := p.peek()
		if t == nil {
			if len(tags) > 0 {
				return nil, p.errorf(tagLine, "tags must apply to a Feature or Scenario")
			}
			break
		}
		if t.Kind != TokFeature {
			if len(tags) > 0 {
				return nil, p.errorf(tagLine, "tags may only apply to Feature or Scenario")
			}
			return nil, p.errorf(t.Line, "expected 'Feature:', got %q", t.Payload)
		}
		f, err := p.parseFeature(tags)
		if err != nil {
			return nil, err
		}
		features = append(features, f)
	}
	return features, nil
}

func (p *parser) parseFeature(tags []string) (Feature, error) {
	t := p.next() // consume Feature:
	feature := Feature{Name: t.Payload, Tags: MergeTags(tags)}

	// Parse optional description / Image: lines until we hit Background:, Scenario:, tags, or Feature:.
	var descLines []string
	for {
		tok := p.peek()
		if tok == nil {
			break
		}
		switch tok.Kind {
		case TokImage:
			p.next()
			feature.Image = tok.Payload
			continue
		case TokBackground, TokScenario, TokFeature, TokTag:
			// done with description
		default:
			// TokBlock lines inside feature description area
			if tok.Kind == TokBlock {
				p.next()
				descLines = append(descLines, tok.Payload)
				continue
			}
		}
		break
	}
	feature.Description = strings.Join(descLines, "\n")

	tagStart := p.pos
	pendingTags, tagLine, err := p.parseTags()
	if err != nil {
		return Feature{}, err
	}
	if len(pendingTags) > 0 {
		if tok := p.peek(); tok != nil && tok.Kind == TokFeature {
			p.pos = tagStart
			pendingTags = nil
			tagLine = 0
		}
	}

	// Optional Background:
	if tok := p.peek(); tok != nil && tok.Kind == TokBackground {
		if len(pendingTags) > 0 {
			return Feature{}, p.errorf(tagLine, "tags may only apply to Feature or Scenario")
		}
		p.next() // consume Background:
		steps, err := p.parseSteps()
		if err != nil {
			return Feature{}, err
		}
		feature.Background = steps
	}

	// One or more Scenario:
	for {
		tagsBelongToNextFeature := false
		if len(pendingTags) == 0 {
			tagStart = p.pos
			pendingTags, tagLine, err = p.parseTags()
			if err != nil {
				return Feature{}, err
			}
			if len(pendingTags) > 0 {
				if tok := p.peek(); tok != nil && tok.Kind == TokFeature {
					p.pos = tagStart
					pendingTags = nil
					tagLine = 0
					tagsBelongToNextFeature = true
				}
			}
		}
		if tagsBelongToNextFeature {
			break
		}

		tok := p.peek()
		if tok == nil || tok.Kind == TokFeature {
			if len(pendingTags) > 0 {
				return Feature{}, p.errorf(tagLine, "tags must apply to a Feature or Scenario")
			}
			break
		}
		if tok.Kind != TokScenario {
			if len(pendingTags) > 0 {
				return Feature{}, p.errorf(tagLine, "tags may only apply to Feature or Scenario")
			}
			return Feature{}, p.errorf(tok.Line, "expected 'Scenario:', got %q", tok.Payload)
		}
		sc, err := p.parseScenario(pendingTags)
		if err != nil {
			return Feature{}, err
		}
		feature.Scenarios = append(feature.Scenarios, sc)
		pendingTags = nil
	}

	return feature, nil
}

func (p *parser) parseScenario(tags []string) (Scenario, error) {
	t := p.next() // consume Scenario:
	sc := Scenario{Name: t.Payload, Tags: MergeTags(tags), Line: t.Line}

	steps, err := p.parseSteps()
	if err != nil {
		return Scenario{}, err
	}
	sc.Steps = steps
	return sc, nil
}

func (p *parser) parseTags() ([]string, int, error) {
	var tags []string
	firstLine := 0

	for {
		tok := p.peek()
		if tok == nil || tok.Kind != TokTag {
			break
		}
		p.next()
		if firstLine == 0 {
			firstLine = tok.Line
		}
		lineTags, err := parseTagLine(tok.Payload)
		if err != nil {
			return nil, 0, p.errorf(tok.Line, "%v", err)
		}
		tags = append(tags, lineTags...)
	}

	return MergeTags(tags), firstLine, nil
}

// parseSteps reads Given/When/Then/And/But steps until a higher-level keyword.
func (p *parser) parseSteps() ([]Step, error) {
	var steps []Step
	lastResolved := StepGiven // fallback for And/But at start (shouldn't happen in valid files)

	for {
		tok := p.peek()
		if tok == nil {
			break
		}

		if tok.Kind != TokGiven && tok.Kind != TokWhen && tok.Kind != TokThen &&
			tok.Kind != TokAnd && tok.Kind != TokBut {
			break
		}

		var stepType StepType
		switch tok.Kind {
		case TokGiven:
			stepType = StepGiven
		case TokWhen:
			stepType = StepWhen
		case TokThen:
			stepType = StepThen
		case TokAnd:
			stepType = StepAnd
		default: // TokBut
			stepType = StepBut
		}

		p.next() // consume keyword token

		resolved := stepType
		if stepType == StepAnd || stepType == StepBut {
			resolved = lastResolved
		} else {
			lastResolved = stepType
		}

		step := Step{
			Type:         stepType,
			ResolvedType: resolved,
			Text:         tok.Payload,
			Line:         tok.Line,
		}

		// Collect multi-line block: following TokBlock tokens with greater indent
		keywordIndent := tok.Indent
		var blockLines []string
		for {
			next := p.peek()
			if next == nil || next.Kind != TokBlock || next.Indent <= keywordIndent {
				break
			}
			p.next()
			blockLines = append(blockLines, next.Payload)
		}
		if len(blockLines) > 0 {
			step.Block = strings.Join(blockLines, "\n")
		}

		steps = append(steps, step)
	}
	return steps, nil
}

func (p *parser) errorf(line int, format string, args ...interface{}) error {
	msg := fmt.Sprintf(format, args...)
	return fmt.Errorf("%s:%d: %s", p.filename, line, msg)
}
