package platform

import (
	"fmt"
	"regexp"
	"strings"
)

const (
	maxKnowledgeCodeBlockLines = 12
	maxKnowledgeCodeRatio      = 0.40
	maxKnowledgeTableRows      = 8
	minKnowledgeSummaryLen     = 15
)

var (
	vagueSummaryRE = regexp.MustCompile(`(?i)^(notes|summary|details|info|misc|update|overview|thoughts)\s+(on|of|for|about)\b`)
	docCopyHeadingRE = regexp.MustCompile(`(?im)^#{1,3}\s+(open[\s-]+questions|changelog|table of contents|todo list)\b`)
)

func checkKnowledgeQuality(template, summary, body string) []string {
	if template != "finding" && template != "decision" {
		return nil
	}
	var problems []string

	if s := strings.TrimSpace(summary); s != "" {
		if len(s) < minKnowledgeSummaryLen {
			problems = append(problems, fmt.Sprintf("summary is too short (%d chars); state the concrete fact or consequence", len(s)))
		} else if vagueSummaryRE.MatchString(s) {
			problems = append(problems, "summary is too vague; state the concrete fact or consequence instead of 'Notes/Summary on ...'")
		}
	}

	if strings.TrimSpace(body) == "" {
		return problems
	}

	if docCopyHeadingRE.MatchString(body) {
		problems = append(problems, "body looks like a copied repo doc section (open questions / changelog / todo); record a verified finding or decision instead")
	}

	lines := strings.Split(body, "\n")
	inFence := false
	fenceLines := 0
	totalCodeLines := 0
	totalNonEmpty := 0
	maxBlock := 0
	tableRows := 0
	maxTableRows := 0

	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "```") || strings.HasPrefix(line, "~~~") {
			if inFence {
				if fenceLines > maxBlock {
					maxBlock = fenceLines
				}
				inFence = false
				fenceLines = 0
			} else {
				inFence = true
				fenceLines = 0
			}
			continue
		}
		if line != "" {
			totalNonEmpty++
		}
		if inFence {
			if line != "" {
				fenceLines++
				totalCodeLines++
			}
			continue
		}
		if strings.HasPrefix(line, "|") && strings.HasSuffix(line, "|") && !strings.Contains(line, "---") {
			tableRows++
			if tableRows > maxTableRows {
				maxTableRows = tableRows
			}
		} else {
			tableRows = 0
		}
	}
	if inFence && fenceLines > maxBlock {
		maxBlock = fenceLines
	}

	if maxBlock > maxKnowledgeCodeBlockLines {
		problems = append(problems, fmt.Sprintf("fenced code block has %d lines (limit %d); explain the constraint or trap instead of pasting code", maxBlock, maxKnowledgeCodeBlockLines))
	} else if totalNonEmpty >= 10 && float64(totalCodeLines)/float64(totalNonEmpty) > maxKnowledgeCodeRatio {
		problems = append(problems, fmt.Sprintf("body is %.0f%% code; knowledge entries must explain why/constraints rather than restating code", 100*float64(totalCodeLines)/float64(totalNonEmpty)))
	}

	if maxTableRows > maxKnowledgeTableRows {
		problems = append(problems, fmt.Sprintf("table has %d rows (limit %d); do not copy repo doc tables into the vault", maxTableRows, maxKnowledgeTableRows))
	}

	return problems
}
