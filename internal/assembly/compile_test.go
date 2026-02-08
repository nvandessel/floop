package assembly

import (
	"strings"
	"testing"

	"github.com/nvandessel/feedback-loop/internal/models"
)

func TestCompiler_Compile_Empty(t *testing.T) {
	compiler := NewCompiler()
	result := compiler.Compile(nil)

	if result.Text != "" {
		t.Errorf("expected empty text, got %q", result.Text)
	}
	if len(result.Sections) != 0 {
		t.Errorf("expected no sections, got %d", len(result.Sections))
	}
	if result.TotalTokens != 0 {
		t.Errorf("expected 0 tokens, got %d", result.TotalTokens)
	}
}

func TestCompiler_Compile_SingleBehavior(t *testing.T) {
	compiler := NewCompiler()
	behaviors := []models.Behavior{
		{
			ID:   "b1",
			Name: "test-behavior",
			Kind: models.BehaviorKindDirective,
			Content: models.BehaviorContent{
				Canonical: "Always use gofmt",
			},
		},
	}

	result := compiler.Compile(behaviors)

	if result.Text == "" {
		t.Error("expected non-empty text")
	}
	if !strings.Contains(result.Text, "Always use gofmt") {
		t.Error("expected text to contain behavior content")
	}
	if len(result.IncludedBehaviors) != 1 {
		t.Errorf("expected 1 included behavior, got %d", len(result.IncludedBehaviors))
	}
	if result.IncludedBehaviors[0] != "b1" {
		t.Errorf("expected included behavior 'b1', got %q", result.IncludedBehaviors[0])
	}
}

func TestCompiler_Compile_MultipleBehaviors(t *testing.T) {
	compiler := NewCompiler()
	behaviors := []models.Behavior{
		{
			ID:   "b1",
			Kind: models.BehaviorKindConstraint,
			Content: models.BehaviorContent{
				Canonical: "Never commit secrets",
			},
		},
		{
			ID:   "b2",
			Kind: models.BehaviorKindDirective,
			Content: models.BehaviorContent{
				Canonical: "Use error wrapping",
			},
		},
		{
			ID:   "b3",
			Kind: models.BehaviorKindPreference,
			Content: models.BehaviorContent{
				Canonical: "Prefer table-driven tests",
			},
		},
	}

	result := compiler.Compile(behaviors)

	// Should have sections for constraints, directives, and preferences
	if len(result.Sections) != 3 {
		t.Errorf("expected 3 sections, got %d", len(result.Sections))
	}

	// Constraints should come first
	if result.Sections[0].Kind != models.BehaviorKindConstraint {
		t.Errorf("expected first section to be constraints, got %s", result.Sections[0].Kind)
	}

	// Check all behaviors are included
	if len(result.IncludedBehaviors) != 3 {
		t.Errorf("expected 3 included behaviors, got %d", len(result.IncludedBehaviors))
	}
}

func TestCompiler_Compile_Markdown(t *testing.T) {
	compiler := NewCompiler().WithFormat(FormatMarkdown)
	behaviors := []models.Behavior{
		{
			ID:   "b1",
			Kind: models.BehaviorKindDirective,
			Content: models.BehaviorContent{
				Canonical: "Use Go 1.25",
			},
		},
	}

	result := compiler.Compile(behaviors)

	if !strings.Contains(result.Text, "## Learned Behaviors") {
		t.Error("expected markdown header")
	}
	if !strings.Contains(result.Text, "### Directives") {
		t.Error("expected directives section header")
	}
	if !strings.Contains(result.Text, "- Use Go 1.25") {
		t.Error("expected bullet point format")
	}
}

func TestCompiler_Compile_XML(t *testing.T) {
	compiler := NewCompiler().WithFormat(FormatXML)
	behaviors := []models.Behavior{
		{
			ID:   "b1",
			Kind: models.BehaviorKindDirective,
			Content: models.BehaviorContent{
				Canonical: "Use Go 1.25",
			},
		},
	}

	result := compiler.Compile(behaviors)

	if !strings.Contains(result.Text, "<learned-behaviors>") {
		t.Error("expected XML root element")
	}
	if !strings.Contains(result.Text, "<behavior kind=\"directive\">") {
		t.Error("expected behavior element with kind attribute")
	}
	if !strings.Contains(result.Text, "</learned-behaviors>") {
		t.Error("expected closing XML root element")
	}
}

func TestCompiler_Compile_Plain(t *testing.T) {
	compiler := NewCompiler().WithFormat(FormatPlain)
	behaviors := []models.Behavior{
		{
			ID:   "b1",
			Kind: models.BehaviorKindDirective,
			Content: models.BehaviorContent{
				Canonical: "Use Go 1.25",
			},
		},
	}

	result := compiler.Compile(behaviors)

	if !strings.Contains(result.Text, "Directives:") {
		t.Error("expected section title")
	}
	if !strings.Contains(result.Text, "Use Go 1.25") {
		t.Error("expected behavior content")
	}
}

func TestCompiler_Compile_WithExpanded(t *testing.T) {
	compiler := NewCompiler().WithExpanded(true)
	behaviors := []models.Behavior{
		{
			ID:   "b1",
			Kind: models.BehaviorKindDirective,
			Content: models.BehaviorContent{
				Canonical: "Use Go 1.25",
				Expanded:  "Always use Go version 1.25 or later for new projects because it includes important performance improvements.",
			},
		},
	}

	result := compiler.Compile(behaviors)

	if !strings.Contains(result.Text, "important performance improvements") {
		t.Error("expected expanded content when WithExpanded is true")
	}
}

func TestCompiler_Compile_SortsByPriority(t *testing.T) {
	compiler := NewCompiler()
	behaviors := []models.Behavior{
		{
			ID:       "low",
			Kind:     models.BehaviorKindDirective,
			Priority: 1,
			Content:  models.BehaviorContent{Canonical: "Low priority"},
		},
		{
			ID:       "high",
			Kind:     models.BehaviorKindDirective,
			Priority: 10,
			Content:  models.BehaviorContent{Canonical: "High priority"},
		},
	}

	result := compiler.Compile(behaviors)

	// High priority should come first in the section
	highIdx := strings.Index(result.Text, "High priority")
	lowIdx := strings.Index(result.Text, "Low priority")

	if highIdx > lowIdx {
		t.Error("expected high priority behavior before low priority")
	}
}

func TestEstimateTokens(t *testing.T) {
	tests := []struct {
		text     string
		expected int
	}{
		{"", 0},
		{"test", 1},                              // 4 chars = 1 token
		{"hello world", 3},                       // 11 chars ≈ 3 tokens
		{"a longer sentence with more words", 9}, // 33 chars ≈ 9 tokens
	}

	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			got := estimateTokens(tt.text)
			if got != tt.expected {
				t.Errorf("estimateTokens(%q) = %d, want %d", tt.text, got, tt.expected)
			}
		})
	}
}

func TestFormatBehaviorXML_EscapesContent(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		wantContain string
		wantAbsent  string
	}{
		{
			name:        "escapes angle brackets",
			content:     "<script>alert('xss')</script>",
			wantContain: "&lt;script&gt;alert(&apos;xss&apos;)&lt;/script&gt;",
			wantAbsent:  "<script>",
		},
		{
			name:        "escapes double quotes in content",
			content:     `"quoted"`,
			wantContain: "&quot;quoted&quot;",
		},
		{
			name:        "escapes ampersand",
			content:     "A & B",
			wantContain: "A &amp; B",
			wantAbsent:  "A & B",
		},
		{
			name:        "escapes single quote",
			content:     "it's fine",
			wantContain: "it&apos;s fine",
		},
		{
			name:        "escapes all special chars combined",
			content:     `<b>"Tom & Jerry's"</b>`,
			wantContain: "&lt;b&gt;&quot;Tom &amp; Jerry&apos;s&quot;&lt;/b&gt;",
		},
		{
			name:        "no special chars unchanged",
			content:     "Use Go 1.25 for all projects",
			wantContain: "Use Go 1.25 for all projects",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler().WithFormat(FormatXML)
			b := models.Behavior{
				ID:   "b1",
				Kind: models.BehaviorKindDirective,
				Content: models.BehaviorContent{
					Canonical: tt.content,
				},
			}

			result := compiler.Compile([]models.Behavior{b})

			if !strings.Contains(result.Text, tt.wantContain) {
				t.Errorf("expected output to contain %q, got %q", tt.wantContain, result.Text)
			}
			if tt.wantAbsent != "" && strings.Contains(result.Text, tt.wantAbsent) {
				t.Errorf("expected output NOT to contain %q, got %q", tt.wantAbsent, result.Text)
			}

			// The kind attribute should NOT be escaped (it's a controlled enum)
			if !strings.Contains(result.Text, `kind="directive"`) {
				t.Errorf("expected kind attribute to remain unescaped, got %q", result.Text)
			}
		})
	}
}

func TestFormatCluster_EscapesXMLContent(t *testing.T) {
	compiler := NewCompiler().WithFormat(FormatXML)

	rep := models.Behavior{
		ID:   "rep1",
		Name: "use-pathlib",
		Kind: models.BehaviorKindDirective,
		Content: models.BehaviorContent{
			Canonical: "Use pathlib.Path <not os.path> for \"all\" file ops",
		},
	}
	member := models.Behavior{
		ID:   "mem1",
		Name: "prefer-context<mgr> & cleanup",
		Kind: models.BehaviorKindDirective,
	}

	cluster := BehaviorCluster{
		Representative: models.InjectedBehavior{
			Behavior: &rep,
			Tier:     models.TierFull,
			Content:  rep.Content.Canonical,
		},
		Members: []models.InjectedBehavior{
			{Behavior: &member, Tier: models.TierNameOnly, Content: member.Name},
		},
		ClusterLabel: "Python <File> Handling",
		SharedTags:   []string{"python"},
	}

	result := compiler.CompileCoalesced(nil, []BehaviorCluster{cluster})

	// Representative content must be escaped
	if strings.Contains(result, "<not os.path>") {
		t.Error("expected representative content to be XML-escaped, found raw angle brackets")
	}
	if !strings.Contains(result, "&lt;not os.path&gt;") {
		t.Errorf("expected escaped representative content, got %q", result)
	}
	if !strings.Contains(result, "&quot;all&quot;") {
		t.Errorf("expected escaped double quotes in content, got %q", result)
	}

	// Member names must be escaped in <also> tag
	if strings.Contains(result, "<mgr>") {
		t.Error("expected member name to be XML-escaped in <also> tag, found raw angle brackets")
	}
	if !strings.Contains(result, "&lt;mgr&gt;") {
		t.Errorf("expected escaped member name, got %q", result)
	}
	if !strings.Contains(result, "&amp;") {
		t.Errorf("expected escaped ampersand in member name, got %q", result)
	}

	// Cluster label must be escaped in attribute context
	if !strings.Contains(result, "Python &lt;File&gt; Handling") || strings.Contains(result, "Python <File> Handling") {
		// The label is inside an attribute, so it could be escaped differently,
		// but at minimum angle brackets must not appear raw
		if strings.Contains(result, `label="Python <File> Handling"`) {
			t.Error("expected cluster label to be XML-escaped in attribute, found raw angle brackets")
		}
	}
}

func TestEscapeXML(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "ampersand",
			input: "A & B",
			want:  "A &amp; B",
		},
		{
			name:  "less than",
			input: "a < b",
			want:  "a &lt; b",
		},
		{
			name:  "greater than",
			input: "a > b",
			want:  "a &gt; b",
		},
		{
			name:  "double quote",
			input: `say "hello"`,
			want:  "say &quot;hello&quot;",
		},
		{
			name:  "single quote",
			input: "it's",
			want:  "it&apos;s",
		},
		{
			name:  "combined entities",
			input: `<b>"Tom & Jerry's"</b>`,
			want:  "&lt;b&gt;&quot;Tom &amp; Jerry&apos;s&quot;&lt;/b&gt;",
		},
		{
			name:  "already escaped entity is double-escaped",
			input: "&lt;",
			want:  "&amp;lt;",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "no special chars",
			input: "hello world 123",
			want:  "hello world 123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := escapeXML(tt.input)
			if got != tt.want {
				t.Errorf("escapeXML(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestCompileTiered_XML_EscapesSummarizedAndNameOnly(t *testing.T) {
	compiler := NewCompiler().WithFormat(FormatXML)

	fullB := models.Behavior{
		ID:   "full-1",
		Kind: models.BehaviorKindDirective,
		Content: models.BehaviorContent{
			Canonical: "Use Go modules",
		},
	}
	summB := models.Behavior{
		ID:   "summ-behavior-1",
		Kind: models.BehaviorKindPreference,
		Content: models.BehaviorContent{
			Canonical: "Prefer interfaces <T> over concrete types",
			Summary:   "Prefer interfaces <T>",
		},
	}
	nameB := models.Behavior{
		ID:   "name-behavior-1",
		Kind: models.BehaviorKindPreference,
		Content: models.BehaviorContent{
			Canonical: "Use context & cancellation",
		},
	}

	plan := &models.InjectionPlan{
		FullBehaviors: []models.InjectedBehavior{
			{Behavior: &fullB, Tier: models.TierFull, Content: fullB.Content.Canonical},
		},
		SummarizedBehaviors: []models.InjectedBehavior{
			{Behavior: &summB, Tier: models.TierSummary, Content: "Prefer interfaces <T>"},
		},
		NameOnlyBehaviors: []models.InjectedBehavior{
			{Behavior: &nameB, Tier: models.TierNameOnly, Content: "Use context & cancellation [preference]"},
		},
		TokenBudget: 500,
	}

	result := compiler.CompileTiered(plan)

	// Summarized content inside <quick-reference> must be escaped
	if strings.Contains(result.Text, "interfaces <T>") {
		t.Errorf("expected summarized content to be XML-escaped inside <quick-reference>, got raw angle brackets in: %s", result.Text)
	}
	if !strings.Contains(result.Text, "interfaces &lt;T&gt;") {
		t.Errorf("expected escaped angle brackets in summarized content, got: %s", result.Text)
	}

	// Name-only content inside <also-available> must be escaped
	if strings.Contains(result.Text, "context & cancellation") {
		t.Errorf("expected name-only content to be XML-escaped inside <also-available>, got raw ampersand in: %s", result.Text)
	}
	if !strings.Contains(result.Text, "context &amp; cancellation") {
		t.Errorf("expected escaped ampersand in name-only content, got: %s", result.Text)
	}
}

func TestCompiler_CompileTiered_Nil(t *testing.T) {
	compiler := NewCompiler()
	result := compiler.CompileTiered(nil)

	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Text != "" {
		t.Errorf("expected empty text, got %q", result.Text)
	}
}

func TestCompiler_CompileTiered_FullOnly(t *testing.T) {
	compiler := NewCompiler()

	b1 := models.Behavior{
		ID:   "b1",
		Kind: models.BehaviorKindDirective,
		Content: models.BehaviorContent{
			Canonical: "Use Go modules",
		},
	}
	b2 := models.Behavior{
		ID:   "b2",
		Kind: models.BehaviorKindConstraint,
		Content: models.BehaviorContent{
			Canonical: "Never commit secrets",
		},
	}

	plan := &models.InjectionPlan{
		FullBehaviors: []models.InjectedBehavior{
			{Behavior: &b1, Tier: models.TierFull, Content: b1.Content.Canonical},
			{Behavior: &b2, Tier: models.TierFull, Content: b2.Content.Canonical},
		},
		TokenBudget: 1000,
	}

	result := compiler.CompileTiered(plan)

	if !strings.Contains(result.Text, "Use Go modules") {
		t.Error("expected full behavior content")
	}
	if !strings.Contains(result.Text, "Never commit secrets") {
		t.Error("expected constraint content")
	}
	if len(result.SummarizedBehaviors) != 0 {
		t.Errorf("expected no summarized behaviors, got %d", len(result.SummarizedBehaviors))
	}
}

func TestCompiler_CompileTiered_WithSummaries(t *testing.T) {
	compiler := NewCompiler()

	b1 := models.Behavior{
		ID:   "b1",
		Kind: models.BehaviorKindDirective,
		Content: models.BehaviorContent{
			Canonical: "Use Go modules for dependency management",
		},
	}
	b2 := models.Behavior{
		ID:   "summarized-behavior",
		Kind: models.BehaviorKindPreference,
		Content: models.BehaviorContent{
			Canonical: "Prefer interfaces over concrete types",
			Summary:   "Prefer interfaces",
		},
	}

	plan := &models.InjectionPlan{
		FullBehaviors: []models.InjectedBehavior{
			{Behavior: &b1, Tier: models.TierFull, Content: b1.Content.Canonical},
		},
		SummarizedBehaviors: []models.InjectedBehavior{
			{Behavior: &b2, Tier: models.TierSummary, Content: "Prefer interfaces"},
		},
		TokenBudget: 500,
	}

	result := compiler.CompileTiered(plan)

	if !strings.Contains(result.Text, "Quick Reference") {
		t.Error("expected quick reference section for summarized behaviors")
	}
	if !strings.Contains(result.Text, "Prefer interfaces") {
		t.Error("expected summary content")
	}
	if len(result.SummarizedBehaviors) != 1 {
		t.Errorf("expected 1 summarized behavior, got %d", len(result.SummarizedBehaviors))
	}
}

func TestCompiler_CompileTiered_WithOmitted(t *testing.T) {
	compiler := NewCompiler()

	b1 := models.Behavior{
		ID:   "full-b1",
		Kind: models.BehaviorKindDirective,
		Content: models.BehaviorContent{
			Canonical: "Full content here",
		},
	}
	b2 := models.Behavior{
		ID:   "omitted-behavior-1",
		Kind: models.BehaviorKindPreference,
	}
	b3 := models.Behavior{
		ID:   "omitted-behavior-2",
		Kind: models.BehaviorKindPreference,
	}

	plan := &models.InjectionPlan{
		FullBehaviors: []models.InjectedBehavior{
			{Behavior: &b1, Tier: models.TierFull, Content: b1.Content.Canonical},
		},
		OmittedBehaviors: []models.InjectedBehavior{
			{Behavior: &b2, Tier: models.TierOmitted},
			{Behavior: &b3, Tier: models.TierOmitted},
		},
		TokenBudget: 100,
	}

	result := compiler.CompileTiered(plan)

	if !strings.Contains(result.Text, "additional behaviors available") {
		t.Error("expected omitted behaviors footer")
	}
	if !strings.Contains(result.Text, "floop show") {
		t.Error("expected floop show hint")
	}
	if len(result.OmittedBehaviors) != 2 {
		t.Errorf("expected 2 omitted behaviors, got %d", len(result.OmittedBehaviors))
	}
}
