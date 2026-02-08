package assembly

import (
	"fmt"
	"sort"
	"strings"

	"github.com/nvandessel/feedback-loop/internal/models"
)

// Format specifies the output format for compiled prompts
type Format string

const (
	FormatMarkdown Format = "markdown"
	FormatXML      Format = "xml"
	FormatPlain    Format = "plain"
)

// CompiledPrompt represents the final assembled prompt section
type CompiledPrompt struct {
	// The formatted prompt text ready for injection
	Text string `json:"text"`

	// Sections organized by behavior kind
	Sections []PromptSection `json:"sections"`

	// Token statistics
	TotalTokens int `json:"total_tokens"`

	// Format used
	Format Format `json:"format"`

	// Behaviors included (for debugging/tracing)
	IncludedBehaviors []string `json:"included_behaviors"`

	// Behaviors excluded due to token limits
	ExcludedBehaviors []string `json:"excluded_behaviors,omitempty"`
}

// PromptSection groups behaviors by kind
type PromptSection struct {
	Kind       models.BehaviorKind `json:"kind"`
	Title      string              `json:"title"`
	Content    string              `json:"content"`
	TokenCount int                 `json:"token_count"`
	Behaviors  []string            `json:"behaviors"` // IDs of included behaviors
}

// Compiler transforms active behaviors into prompt-ready format
type Compiler struct {
	format      Format
	useExpanded bool // Use expanded content when available
}

// NewCompiler creates a new behavior compiler
func NewCompiler() *Compiler {
	return &Compiler{
		format:      FormatMarkdown,
		useExpanded: false, // Default to canonical (token-efficient)
	}
}

// WithFormat sets the output format
func (c *Compiler) WithFormat(format Format) *Compiler {
	c.format = format
	return c
}

// WithExpanded uses expanded content when available
func (c *Compiler) WithExpanded(useExpanded bool) *Compiler {
	c.useExpanded = useExpanded
	return c
}

// Compile transforms active behaviors into a prompt-ready format
func (c *Compiler) Compile(behaviors []models.Behavior) *CompiledPrompt {
	if len(behaviors) == 0 {
		return &CompiledPrompt{
			Text:              "",
			Sections:          []PromptSection{},
			TotalTokens:       0,
			Format:            c.format,
			IncludedBehaviors: []string{},
		}
	}

	// Group behaviors by kind
	grouped := c.groupByKind(behaviors)

	// Build sections
	sections := c.buildSections(grouped)

	// Assemble final text
	text := c.assembleText(sections)

	// Collect behavior IDs
	var includedIDs []string
	for _, b := range behaviors {
		includedIDs = append(includedIDs, b.ID)
	}

	return &CompiledPrompt{
		Text:              text,
		Sections:          sections,
		TotalTokens:       estimateTokens(text),
		Format:            c.format,
		IncludedBehaviors: includedIDs,
	}
}

// groupByKind organizes behaviors by their kind
func (c *Compiler) groupByKind(behaviors []models.Behavior) map[models.BehaviorKind][]models.Behavior {
	grouped := make(map[models.BehaviorKind][]models.Behavior)

	for _, b := range behaviors {
		grouped[b.Kind] = append(grouped[b.Kind], b)
	}

	// Sort within each group by priority (descending) then confidence (descending)
	for kind := range grouped {
		sort.Slice(grouped[kind], func(i, j int) bool {
			if grouped[kind][i].Priority != grouped[kind][j].Priority {
				return grouped[kind][i].Priority > grouped[kind][j].Priority
			}
			return grouped[kind][i].Confidence > grouped[kind][j].Confidence
		})
	}

	return grouped
}

// buildSections creates prompt sections from grouped behaviors
func (c *Compiler) buildSections(grouped map[models.BehaviorKind][]models.Behavior) []PromptSection {
	// Define order of sections (constraints first as they're most important)
	kindOrder := []models.BehaviorKind{
		models.BehaviorKindConstraint,
		models.BehaviorKindDirective,
		models.BehaviorKindPreference,
		models.BehaviorKindProcedure,
	}

	var sections []PromptSection

	for _, kind := range kindOrder {
		behaviors, exists := grouped[kind]
		if !exists || len(behaviors) == 0 {
			continue
		}

		section := PromptSection{
			Kind:      kind,
			Title:     c.kindTitle(kind),
			Behaviors: make([]string, 0, len(behaviors)),
		}

		var contentParts []string
		for _, b := range behaviors {
			content := c.formatBehavior(b)
			contentParts = append(contentParts, content)
			section.Behaviors = append(section.Behaviors, b.ID)
		}

		section.Content = strings.Join(contentParts, "\n")
		section.TokenCount = estimateTokens(section.Content)
		sections = append(sections, section)
	}

	return sections
}

// kindTitle returns a human-readable title for a behavior kind
func (c *Compiler) kindTitle(kind models.BehaviorKind) string {
	switch kind {
	case models.BehaviorKindConstraint:
		return "Constraints"
	case models.BehaviorKindDirective:
		return "Directives"
	case models.BehaviorKindPreference:
		return "Preferences"
	case models.BehaviorKindProcedure:
		return "Procedures"
	default:
		return "Behaviors"
	}
}

// formatBehavior formats a single behavior for the prompt
func (c *Compiler) formatBehavior(b models.Behavior) string {
	// Choose content based on settings
	content := b.Content.Canonical
	if c.useExpanded && b.Content.Expanded != "" {
		content = b.Content.Expanded
	}

	switch c.format {
	case FormatXML:
		return c.formatBehaviorXML(b, content)
	case FormatPlain:
		return c.formatBehaviorPlain(b, content)
	default: // FormatMarkdown
		return c.formatBehaviorMarkdown(b, content)
	}
}

func (c *Compiler) formatBehaviorMarkdown(b models.Behavior, content string) string {
	// Format with bullet point
	return fmt.Sprintf("- %s", content)
}

func (c *Compiler) formatBehaviorXML(b models.Behavior, content string) string {
	return fmt.Sprintf("<behavior kind=\"%s\">%s</behavior>", b.Kind, escapeXML(content))
}

// escapeXML escapes XML special characters in content strings.
func escapeXML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;") // Must be first!
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	s = strings.ReplaceAll(s, "'", "&apos;")
	return s
}

func (c *Compiler) formatBehaviorPlain(b models.Behavior, content string) string {
	return content
}

// assembleText combines sections into final prompt text
func (c *Compiler) assembleText(sections []PromptSection) string {
	if len(sections) == 0 {
		return ""
	}

	var parts []string

	switch c.format {
	case FormatXML:
		parts = append(parts, "<learned-behaviors>")
		for _, s := range sections {
			parts = append(parts, fmt.Sprintf("<%s>", strings.ToLower(s.Title)))
			parts = append(parts, s.Content)
			parts = append(parts, fmt.Sprintf("</%s>", strings.ToLower(s.Title)))
		}
		parts = append(parts, "</learned-behaviors>")

	case FormatPlain:
		for i, s := range sections {
			if i > 0 {
				parts = append(parts, "")
			}
			parts = append(parts, s.Title+":")
			parts = append(parts, s.Content)
		}

	default: // FormatMarkdown
		parts = append(parts, "## Learned Behaviors")
		parts = append(parts, "")
		for _, s := range sections {
			parts = append(parts, fmt.Sprintf("### %s", s.Title))
			parts = append(parts, s.Content)
			parts = append(parts, "")
		}
	}

	return strings.TrimSpace(strings.Join(parts, "\n"))
}

// estimateTokens provides a rough token count estimate
// Uses the common heuristic of ~4 characters per token
func estimateTokens(text string) int {
	if text == "" {
		return 0
	}
	// Rough estimate: 1 token ≈ 4 characters for English text
	return (len(text) + 3) / 4
}

// TieredCompiledPrompt extends CompiledPrompt with tiering information
type TieredCompiledPrompt struct {
	CompiledPrompt

	// SummarizedBehaviors are behaviors included as summaries
	SummarizedBehaviors []string `json:"summarized_behaviors,omitempty"`

	// NameOnlyBehaviorIDs are behaviors included as name + kind + tags only
	NameOnlyBehaviorIDs []string `json:"name_only_behaviors,omitempty"`

	// OmittedBehaviors are behaviors referenced but not included
	OmittedBehaviors []string `json:"omitted_behaviors,omitempty"`

	// QuickReferenceSection contains summarized behaviors
	QuickReferenceSection string `json:"quick_reference_section,omitempty"`

	// NameOnlySection contains name-only behaviors
	NameOnlySection string `json:"name_only_section,omitempty"`
}

// CompileTiered transforms an injection plan into a tiered prompt
func (c *Compiler) CompileTiered(plan *models.InjectionPlan) *TieredCompiledPrompt {
	if plan == nil {
		return &TieredCompiledPrompt{
			CompiledPrompt: CompiledPrompt{
				Text:              "",
				Sections:          []PromptSection{},
				TotalTokens:       0,
				Format:            c.format,
				IncludedBehaviors: []string{},
			},
		}
	}

	// Compile full-tier behaviors normally
	fullBehaviors := make([]models.Behavior, 0, len(plan.FullBehaviors))
	for _, ib := range plan.FullBehaviors {
		if ib.Behavior != nil {
			fullBehaviors = append(fullBehaviors, *ib.Behavior)
		}
	}

	basePrompt := c.Compile(fullBehaviors)

	// Build tiered prompt
	result := &TieredCompiledPrompt{
		CompiledPrompt: *basePrompt,
	}

	// Collect IDs
	for _, ib := range plan.SummarizedBehaviors {
		if ib.Behavior != nil {
			result.SummarizedBehaviors = append(result.SummarizedBehaviors, ib.Behavior.ID)
		}
	}
	for _, ib := range plan.NameOnlyBehaviors {
		if ib.Behavior != nil {
			result.NameOnlyBehaviorIDs = append(result.NameOnlyBehaviorIDs, ib.Behavior.ID)
		}
	}
	for _, ib := range plan.OmittedBehaviors {
		if ib.Behavior != nil {
			result.OmittedBehaviors = append(result.OmittedBehaviors, ib.Behavior.ID)
		}
	}

	// Build quick reference section for summarized behaviors
	if len(plan.SummarizedBehaviors) > 0 {
		result.QuickReferenceSection = c.buildQuickReferenceSection(plan.SummarizedBehaviors)
	}

	// Build name-only section
	if len(plan.NameOnlyBehaviors) > 0 {
		result.NameOnlySection = c.buildNameOnlySection(plan.NameOnlyBehaviors)
	}

	// Assemble final text with quick reference and name-only section
	result.Text = c.assembleTieredText(basePrompt.Text, result.QuickReferenceSection, result.NameOnlySection, plan.OmittedBehaviors)
	result.TotalTokens = estimateTokens(result.Text)

	return result
}

// buildQuickReferenceSection creates the summarized behaviors section
func (c *Compiler) buildQuickReferenceSection(summarized []models.InjectedBehavior) string {
	if len(summarized) == 0 {
		return ""
	}

	var lines []string
	for _, ib := range summarized {
		if ib.Behavior == nil {
			continue
		}
		// Format: [short-id] summary content
		shortID := ib.Behavior.ID
		if len(shortID) > 8 {
			shortID = shortID[:8]
		}
		content := ib.Content
		if c.format == FormatXML {
			content = escapeXML(content)
			shortID = escapeXML(shortID)
		}
		lines = append(lines, fmt.Sprintf("- [%s] %s", shortID, content))
	}

	return strings.Join(lines, "\n")
}

// buildNameOnlySection creates the name-only behaviors section.
// Format: `{name}` [{kind}] #tag1 #tag2
func (c *Compiler) buildNameOnlySection(nameOnly []models.InjectedBehavior) string {
	if len(nameOnly) == 0 {
		return ""
	}

	var lines []string
	for _, ib := range nameOnly {
		if ib.Behavior == nil {
			continue
		}
		// Content is pre-formatted by the tier mapper as `name` [kind] #tags
		content := ib.Content
		if c.format == FormatXML {
			content = escapeXML(content)
		}
		lines = append(lines, fmt.Sprintf("- %s", content))
	}

	return strings.Join(lines, "\n")
}

// assembleTieredText combines full content, quick reference, name-only, and omitted info
func (c *Compiler) assembleTieredText(fullText, quickRef, nameOnly string, omitted []models.InjectedBehavior) string {
	var parts []string

	// Add full content
	if fullText != "" {
		parts = append(parts, fullText)
	}

	// Add quick reference section
	if quickRef != "" {
		switch c.format {
		case FormatXML:
			parts = append(parts, "<quick-reference>")
			parts = append(parts, quickRef)
			parts = append(parts, "</quick-reference>")
		case FormatPlain:
			parts = append(parts, "")
			parts = append(parts, "Quick Reference (ask for details if needed):")
			parts = append(parts, quickRef)
		default: // FormatMarkdown
			parts = append(parts, "")
			parts = append(parts, "### Quick Reference (ask for details if needed)")
			parts = append(parts, quickRef)
		}
	}

	// Add name-only section
	if nameOnly != "" {
		switch c.format {
		case FormatXML:
			parts = append(parts, "<also-available>")
			parts = append(parts, nameOnly)
			parts = append(parts, "</also-available>")
		case FormatPlain:
			parts = append(parts, "")
			parts = append(parts, "Also Available (activate with floop show <id>):")
			parts = append(parts, nameOnly)
		default: // FormatMarkdown
			parts = append(parts, "")
			parts = append(parts, "### Also Available (activate with floop show <id>)")
			parts = append(parts, nameOnly)
		}
	}

	// Add omitted behaviors footer
	if len(omitted) > 0 {
		var omittedIDs []string
		for _, ib := range omitted {
			if ib.Behavior != nil {
				shortID := ib.Behavior.ID
				if len(shortID) > 8 {
					shortID = shortID[:8]
				}
				omittedIDs = append(omittedIDs, shortID)
			}
		}

		if len(omittedIDs) > 0 {
			// Show first few IDs
			displayIDs := omittedIDs
			if len(displayIDs) > 3 {
				displayIDs = displayIDs[:3]
			}

			footer := fmt.Sprintf("*%d additional behaviors available: floop show %s...*",
				len(omitted), strings.Join(displayIDs, ", "))

			switch c.format {
			case FormatXML:
				parts = append(parts, fmt.Sprintf("<omitted count=\"%d\"/>", len(omitted)))
			case FormatPlain:
				parts = append(parts, "")
				parts = append(parts, footer)
			default: // FormatMarkdown
				parts = append(parts, "")
				parts = append(parts, footer)
			}
		}
	}

	return strings.TrimSpace(strings.Join(parts, "\n"))
}

// CompileCoalesced renders both individual behaviors and clusters.
//
// Clusters are shown as:
//
//	### Python File Handling (3 behaviors)
//	- **Use pathlib.Path** instead of os.path for all file operations
//	- _Also: prefer-context-managers, avoid-os-walk_ (use `floop show <id>` for details)
//
// Individual behaviors are rendered normally using the standard Compile method.
func (c *Compiler) CompileCoalesced(individuals []models.InjectedBehavior, clusters []BehaviorCluster) string {
	var parts []string

	// Render individual behaviors using the standard compiler.
	if len(individuals) > 0 {
		behaviors := make([]models.Behavior, 0, len(individuals))
		for _, ib := range individuals {
			if ib.Behavior != nil {
				behaviors = append(behaviors, *ib.Behavior)
			}
		}
		if compiled := c.Compile(behaviors); compiled.Text != "" {
			parts = append(parts, compiled.Text)
		}
	}

	// Render clusters.
	if len(clusters) > 0 {
		for _, cluster := range clusters {
			totalCount := 1 + len(cluster.Members) // representative + members
			clusterText := c.formatCluster(cluster, totalCount)
			if clusterText != "" {
				parts = append(parts, clusterText)
			}
		}
	}

	if len(parts) == 0 {
		return ""
	}

	return strings.TrimSpace(strings.Join(parts, "\n\n"))
}

// formatCluster renders a single behavior cluster.
func (c *Compiler) formatCluster(cluster BehaviorCluster, totalCount int) string {
	var lines []string

	switch c.format {
	case FormatXML:
		lines = append(lines, fmt.Sprintf("<cluster label=\"%s\" count=\"%d\">", escapeXML(cluster.ClusterLabel), totalCount))
		if cluster.Representative.Behavior != nil {
			lines = append(lines, fmt.Sprintf("  <behavior kind=\"%s\">%s</behavior>",
				cluster.Representative.Behavior.Kind,
				escapeXML(cluster.Representative.Content)))
		}
		if len(cluster.Members) > 0 {
			var names []string
			for _, m := range cluster.Members {
				if m.Behavior != nil {
					names = append(names, escapeXML(m.Behavior.Name))
				}
			}
			lines = append(lines, fmt.Sprintf("  <also>%s</also>", strings.Join(names, ", ")))
		}
		lines = append(lines, "</cluster>")

	case FormatPlain:
		lines = append(lines, fmt.Sprintf("%s (%d behaviors):", cluster.ClusterLabel, totalCount))
		if cluster.Representative.Content != "" {
			lines = append(lines, fmt.Sprintf("  %s", cluster.Representative.Content))
		}
		if len(cluster.Members) > 0 {
			var names []string
			for _, m := range cluster.Members {
				if m.Behavior != nil {
					names = append(names, m.Behavior.Name)
				}
			}
			lines = append(lines, fmt.Sprintf("  Also: %s (use `floop show <id>` for details)", strings.Join(names, ", ")))
		}

	default: // FormatMarkdown
		lines = append(lines, fmt.Sprintf("### %s (%d behaviors)", cluster.ClusterLabel, totalCount))
		if cluster.Representative.Content != "" {
			lines = append(lines, fmt.Sprintf("- **%s**", cluster.Representative.Content))
		}
		if len(cluster.Members) > 0 {
			var names []string
			for _, m := range cluster.Members {
				if m.Behavior != nil {
					names = append(names, m.Behavior.Name)
				}
			}
			lines = append(lines, fmt.Sprintf("- _Also: %s_ (use `floop show <id>` for details)", strings.Join(names, ", ")))
		}
	}

	return strings.Join(lines, "\n")
}
