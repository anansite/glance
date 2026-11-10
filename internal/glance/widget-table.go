package glance

import (
	"context"
	"fmt"
	"html/template"
	"strconv"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

var tableWidgetTemplate = mustParseTemplate("table.html", "widget-base.html")

type tableHighlight struct {
	GreaterThan *float64 `yaml:"gt"`
	LessThan    *float64 `yaml:"lt"`
	Color       string   `yaml:"color"`
}

type tableColumn struct {
	Field     string          `yaml:"field"`
	Label     string          `yaml:"label"`
	Format    string          `yaml:"format"`
	Decimals  *int            `yaml:"decimals"`
	Align     string          `yaml:"align"`
	Width     string          `yaml:"width"`
	Class     string          `yaml:"class"`
	Highlight *tableHighlight `yaml:"highlight"`
}

type tableCell struct {
	Text  string
	Class string
}

type tableRow struct {
	Cells []tableCell
}

type tableWidget struct {
	widgetBase        `yaml:",inline"`
	*CustomAPIRequest `yaml:",inline"`

	DataPath  string        `yaml:"data-path"`
	Columns   []tableColumn `yaml:"columns"`
	Limit     int           `yaml:"limit"`
	ShowIndex bool          `yaml:"show-index"`

	Rows      []tableRow `yaml:"-"`
	Headers   []string   `yaml:"-"`
	Truncated bool       `yaml:"-"`
}

func (widget *tableWidget) initialize() error {
	widget.withTitle("Table").withCacheDuration(10 * time.Second)

	if err := widget.CustomAPIRequest.initialize(); err != nil {
		return fmt.Errorf("initializing request: %v", err)
	}

	if widget.DataPath == "" {
		return fmt.Errorf("data-path is required")
	}

	if len(widget.Columns) == 0 {
		return fmt.Errorf("at least one column is required")
	}

	headers := make([]string, 0, len(widget.Columns)+1)
	if widget.ShowIndex {
		headers = append(headers, "#")
	}

	for i := range widget.Columns {
		column := &widget.Columns[i]

		if column.Field == "" {
			return fmt.Errorf("column %d is missing 'field'", i)
		}

		if column.Label == "" {
			column.Label = column.Field
		}

		switch column.Format {
		case "", "text", "number", "percent":
		default:
			return fmt.Errorf("column %d has unsupported format %q", i, column.Format)
		}

		switch column.Align {
		case "", "left", "right", "center":
		default:
			return fmt.Errorf("column %d has unsupported align %q", i, column.Align)
		}

		headers = append(headers, column.Label)
	}

	widget.Headers = headers

	return nil
}

func (widget *tableWidget) update(ctx context.Context) {
	resp, err := fetchCustomAPIResponse(ctx, widget.CustomAPIRequest)
	if !widget.canContinueUpdateAfterHandlingErr(err) {
		return
	}

	node := resp.JSON.Get(widget.DataPath)
	if !node.IsArray() {
		widget.withError(fmt.Errorf("data-path %q did not resolve to a JSON array", widget.DataPath))
		return
	}

	rows := make([]tableRow, 0, len(widget.Columns)+1)
	widget.Truncated = false

	for index, item := range node.Array() {
		if widget.Limit > 0 && len(rows) >= widget.Limit {
			widget.Truncated = true
			break
		}

		cells := make([]tableCell, 0, len(widget.Columns)+1)

		if widget.ShowIndex {
			cells = append(cells, tableCell{Text: strconv.Itoa(index + 1), Class: "color-base"})
		}

		for _, column := range widget.Columns {
			value := item.Get(column.Field)
			cells = append(cells, tableCell{
				Text:  formatTableCell(value, column),
				Class: tableCellClass(value, column),
			})
		}

		rows = append(rows, tableRow{Cells: cells})
	}

	widget.Rows = rows
}

func formatTableCell(value gjson.Result, column tableColumn) string {
	decimals := 0
	if column.Decimals != nil {
		decimals = *column.Decimals
	}

	switch column.Format {
	case "number":
		if column.Decimals == nil {
			decimals = 0
		}
		return strconv.FormatFloat(value.Float(), 'f', decimals, 64)
	case "percent":
		if column.Decimals == nil {
			decimals = 1
		}
		return strconv.FormatFloat(value.Float(), 'f', decimals, 64) + "%"
	default:
		return value.String()
	}
}

func tableCellClass(value gjson.Result, column tableColumn) string {
	classes := make([]string, 0, 2)

	if column.Class != "" {
		classes = append(classes, column.Class)
	}

	if h := column.Highlight; h != nil {
		number := value.Float()
		matched := (h.GreaterThan != nil && number > *h.GreaterThan) ||
			(h.LessThan != nil && number < *h.LessThan)

		if matched {
			switch h.Color {
			case "negative":
				classes = append(classes, "color-negative")
			case "positive":
				classes = append(classes, "color-positive")
			default:
				classes = append(classes, "color-highlight")
			}
		}
	}

	return strings.Join(classes, " ")
}

func (widget *tableWidget) Render() template.HTML {
	return widget.renderTemplate(widget, tableWidgetTemplate)
}
