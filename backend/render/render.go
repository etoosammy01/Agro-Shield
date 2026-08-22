package render

import (
	"html/template"
	"net/http"
	"os"
)

func loadTemplates() *template.Template {
	for _, pattern := range []string{"../frontend/pages/*.html", "../../frontend/pages/*.html", "frontend/pages/*.html"} {
		if matches, err := os.ReadDir("."); err == nil && matches != nil {
			if templates, parseErr := template.ParseGlob(pattern); parseErr == nil {
				return templates
			}
		}
	}
	return template.Must(template.ParseGlob("../frontend/pages/*.html"))
}

var TMPL = loadTemplates()

func RenderTemplates(w http.ResponseWriter, name string, data any) error {
	return TMPL.ExecuteTemplate(w, name, data)
}
