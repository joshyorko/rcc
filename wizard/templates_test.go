package wizard

import (
	"testing"
)

func TestFilterTemplates(t *testing.T) {
	templates := []Template{
		{
			Name:        "python",
			DisplayName: "Python Automation",
			Description: "Standard Python-based automation robot",
			Files:       []string{"robot.yaml", "tasks.py"},
		},
		{
			Name:        "standard",
			DisplayName: "Standard Robot Framework",
			Description: "Standard Robot Framework template",
			Files:       []string{"robot.yaml", "tasks.robot"},
		},
		{
			Name:        "extended",
			DisplayName: "Extended Robot Framework",
			Description: "Extended Robot Framework template",
			Files:       []string{"robot.yaml", "tasks.robot", "keywords.robot"},
		},
	}

	tests := []struct {
		name     string
		query    string
		expected int
	}{
		{"empty query returns all", "", 3},
		{"match by name", "python", 1},
		{"match by description case insensitive", "FRAMEWORK", 2},
		{"no match returns empty", "nonexistent", 0},
		{"partial match", "robot", 3}, // matches "robot" in descriptions and files references
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FilterTemplates(templates, tt.query)
			if len(result) != tt.expected {
				t.Errorf("FilterTemplates(%q) returned %d templates, expected %d",
					tt.query, len(result), tt.expected)
			}
		})
	}
}

func TestTemplate(t *testing.T) {
	template := Template{
		Name:        "python",
		DisplayName: "Python Automation",
		Description: "Standard Python-based automation",
		Files:       []string{"robot.yaml", "tasks.py", "conda.yaml"},
	}

	if template.Name != "python" {
		t.Errorf("Expected Name to be 'python', got '%s'", template.Name)
	}

	if len(template.Files) != 3 {
		t.Errorf("Expected 3 files, got %d", len(template.Files))
	}
}

func TestLoadTemplates(t *testing.T) {
	// Test loading internal templates
	templates, err := LoadTemplates(true)
	if err != nil {
		t.Fatalf("LoadTemplates(true) failed: %v", err)
	}

	if len(templates) == 0 {
		t.Error("LoadTemplates(true) returned no templates")
	}

	// Verify template structure
	for _, tmpl := range templates {
		if tmpl.Name == "" {
			t.Error("Template has empty Name")
		}
		if tmpl.DisplayName == "" {
			t.Error("Template has empty DisplayName")
		}
		// Description is optional
		// Files list may be empty if we can't read the template
	}
}

func TestFilterTemplatesCaseInsensitive(t *testing.T) {
	templates := []Template{
		{
			Name:        "Python",
			DisplayName: "Python Automation",
			Description: "Standard PYTHON-based automation robot",
		},
	}

	// Test case-insensitive matching
	tests := []string{"python", "PYTHON", "Python", "pYtHoN"}
	for _, query := range tests {
		result := FilterTemplates(templates, query)
		if len(result) != 1 {
			t.Errorf("FilterTemplates(%q) should match case-insensitively, got %d results",
				query, len(result))
		}
	}
}

func TestFilterTemplatesMultipleMatches(t *testing.T) {
	templates := []Template{
		{Name: "python", Description: "Python robot"},
		{Name: "python-extended", Description: "Extended Python robot"},
		{Name: "standard", Description: "Standard framework"},
	}

	result := FilterTemplates(templates, "python")
	if len(result) != 2 {
		t.Errorf("Expected 2 matches for 'python', got %d", len(result))
	}
}
