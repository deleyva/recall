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
			"mul": func(a, b interface{}) float64 {
				af := toFloat(a)
				bf := toFloat(b)
				return af * bf
			},
			"div": func(a, b interface{}) float64 {
				af := toFloat(a)
				bf := toFloat(b)
				if bf == 0 {
					return 0
				}
				return af / bf
			},
			"seq": func(start, end int) []int {
				var s []int
				for i := start; i <= end; i++ {
					s = append(s, i)
				}
				return s
			},
			"dict": func(values ...interface{}) map[string]interface{} {
				m := make(map[string]interface{})
				for i := 0; i+1 < len(values); i += 2 {
					key, ok := values[i].(string)
					if ok {
						m[key] = values[i+1]
					}
				}
				return m
			},
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

func toFloat(v interface{}) float64 {
	switch n := v.(type) {
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case float64:
		return n
	case float32:
		return float64(n)
	default:
		return 0
	}
}

func (r *Registry) ExecuteTemplate(w io.Writer, name string, data interface{}) error {
	t, ok := r.templates[name]
	if !ok {
		return fmt.Errorf("template %q not found", name)
	}
	return t.ExecuteTemplate(w, name, data)
}
