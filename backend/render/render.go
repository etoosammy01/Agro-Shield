package render

import (
	"encoding/json"
	"fmt"
	"html/template"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
)

// groupThousands inserts separators into a string of digits, e.g. "1250000"
// becomes "1,250,000". The input must carry no sign — callers strip it first.
func groupThousands(digits string) string {
	for i := len(digits) - 3; i > 0; i -= 3 {
		digits = digits[:i] + "," + digits[i:]
	}
	return digits
}

func commaNumber(value float64, decimals int) string {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return "0"
	}
	s := strconv.FormatFloat(value, 'f', decimals, 64)
	parts := strings.SplitN(s, ".", 2)
	whole := parts[0]
	sign := ""
	if strings.HasPrefix(whole, "-") {
		sign, whole = "-", whole[1:]
	}
	whole = groupThousands(whole)
	if decimals == 0 {
		return sign + whole
	}
	return sign + whole + "." + parts[1]
}

// Money renders a naira amount for display: thousands separated, with kobo
// shown whenever the amount actually has kobo. A whole amount loses its
// trailing ".00" (₦4,000); anything with kobo keeps both digits (₦4,000.50).
// Kobo is never rounded away, so a displayed balance always equals the amount
// that can actually be withdrawn.
//
// It accepts any so every amount in the app can reach the page through this
// one function: the wallet queries return numeric::text strings ("4000.00"),
// while the dashboard figures are computed as float64. Templates must not
// print an amount field directly — numeric::text yields "4000.00", which
// neither separates thousands nor follows the kobo rule.
func Money(value any) string {
	amount, ok := amountToFloat(value)
	if !ok {
		return ""
	}
	if math.IsNaN(amount) || math.IsInf(amount, 0) {
		return "0"
	}

	// Work in kobo so "does this amount have kobo?" is an exact integer test.
	// Asking whether the float is non-zero would misfire on values that only
	// look whole in binary floating point.
	kobo := int64(math.Round(math.Abs(amount) * 100))
	if kobo == 0 {
		return "0"
	}

	sign := ""
	if amount < 0 {
		sign = "-"
	}
	rendered := groupThousands(strconv.FormatInt(kobo/100, 10))
	if cents := kobo % 100; cents != 0 {
		rendered = fmt.Sprintf("%s.%02d", rendered, cents)
	}
	return sign + rendered
}

// amountToFloat accepts the shapes an amount arrives in: float64 from computed
// figures, string from Postgres numeric::text, and the integer types that get
// passed through from counts and kobo totals.
func amountToFloat(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case json.Number:
		f, err := v.Float64()
		return f, err == nil
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			return 0, false
		}
		f, err := strconv.ParseFloat(trimmed, 64)
		return f, err == nil
	}
	return 0, false
}

func hasCoordinates(latitude, longitude float64) bool {
	return latitude != 0 && longitude != 0
}

func loadTemplates() *template.Template {
	funcs := template.FuncMap{
		"money": Money,
		// money0 predates the single-format rule. It is kept registered so the
		// templates that still call it keep working, and renders identically to
		// money — including kobo.
		"money0":         Money,
		"comma":          func(v int) string { return commaNumber(float64(v), 0) },
		"hasCoordinates": hasCoordinates,
	}
	for _, pattern := range []string{"../frontend/pages/*.html", "../../frontend/pages/*.html", "frontend/pages/*.html"} {
		if matches, err := os.ReadDir("."); err == nil && matches != nil {
			if templates, parseErr := template.New("base").Funcs(funcs).ParseGlob(pattern); parseErr == nil {
				return templates
			}
		}
	}
	return template.Must(template.New("base").Funcs(funcs).ParseGlob("../frontend/pages/*.html"))
}

var TMPL = loadTemplates()

func RenderTemplates(w http.ResponseWriter, name string, data any) error {
	return TMPL.ExecuteTemplate(w, name, data)
}


// JSON writes v as a JSON response with the given status code.
func JSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}