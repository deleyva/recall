package templates

import (
	"fmt"
	"html/template"
	"io"
	"math"
	"path/filepath"
)

// Registry holds per-page parsed templates to avoid the Go template
// issue where multiple files defining {{define "content"}} conflict.
type Registry struct {
	templates map[string]*template.Template
	funcs     template.FuncMap
}

func NewRegistry() *Registry {
	return &Registry{
		templates: make(map[string]*template.Template),
		funcs: template.FuncMap{
			"minus":    func(a, b int) int { return a - b },
			"plus":     func(a, b int) int { return a + b },
			"mod":      func(a, b int) int { return a % b },
			"ceil":     func(a, b float64) int { return int(math.Ceil(a / b)) },
			"safeHTML": func(s string) template.HTML { return template.HTML(s) },
		},
	}
}

func (r *Registry) Load(templatesDir string) error {
	layouts, err := filepath.Glob(filepath.Join(templatesDir, "layouts", "*.html"))
	if err != nil {
		return err
	}
	partials, err := filepath.Glob(filepath.Join(templatesDir, "partials", "*.html"))
	if err != nil {
		return err
	}

	shared := append(layouts, partials...)

	// Parse each page template with its own copy of layouts + partials
	pages, err := filepath.Glob(filepath.Join(templatesDir, "*.html"))
	if err != nil {
		return err
	}

	for _, page := range pages {
		name := filepath.Base(page)
		files := append([]string{page}, shared...)
		t, err := template.New(name).Funcs(r.funcs).ParseFiles(files...)
		if err != nil {
			return err
		}
		r.templates[name] = t
	}

	// Also register partials standalone (for HTMX fragment responses)
	for _, partial := range partials {
		name := filepath.Base(partial)
		if _, exists := r.templates[name]; exists {
			continue
		}
		t, err := template.New(name).Funcs(r.funcs).ParseFiles(partial)
		if err != nil {
			return err
		}
		r.templates[name] = t
	}

	return nil
}

func (r *Registry) ExecuteTemplate(w io.Writer, name string, data interface{}) error {
	t, ok := r.templates[name]
	if !ok {
		return fmt.Errorf("template %q not found", name)
	}
	return t.ExecuteTemplate(w, name, data)
}
