package admin

import (
	_ "embed"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

//go:embed templates/dashboard.html
var dashboardHTML string

//go:embed assets/app.css
var appCSS string

var dashboard = template.Must(template.New("dashboard").Funcs(template.FuncMap{
	"indexURL": indexURL,
	"issueURL": issueURL,
	"date":     displayDate,
}).Parse(dashboardHTML))

func indexURL(query, status string, offset int) string {
	values := url.Values{}
	if query != "" {
		values.Set("q", query)
	}
	if status != "" {
		values.Set("status", status)
	}
	if offset > 0 {
		values.Set("offset", strconv.Itoa(offset))
	}
	if len(values) == 0 {
		return "/"
	}
	return "/?" + values.Encode()
}

func issueURL(id, query, status string, listOffset, offset int) string {
	values := url.Values{}
	if query != "" {
		values.Set("q", query)
	}
	if status != "" {
		values.Set("status", status)
	}
	if listOffset > 0 {
		values.Set("list_offset", strconv.Itoa(listOffset))
	}
	if offset > 0 {
		values.Set("offset", strconv.Itoa(offset))
	}
	if len(values) == 0 {
		return "/issues/" + id
	}
	return "/issues/" + id + "?" + values.Encode()
}

func displayDate(value string) string {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05.999999999-07:00", "2006-01-02 15:04:05"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC().Format("Jan 2, 2006 · 15:04 UTC")
		}
	}
	return value
}

func render(w http.ResponseWriter, p page) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = dashboard.Execute(w, struct {
		page
		Styles template.CSS
	}{p, template.CSS(appCSS)})
}
