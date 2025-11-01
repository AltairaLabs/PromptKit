# HTML Templates

The HTML report template is embedded in the Go binary using the `embed` directive.

To modify the template:
1. Edit the `report.html.tmpl` file in this directory
2. Run `go generate` or rebuild the application to embed the changes

The template uses Go's html/template syntax with custom functions defined in `html.go`.
