package glance

import (
	"context"
	"fmt"
	"html/template"
	"math"
	"strconv"
	"time"

	"github.com/tidwall/gjson"
)

var metricsWidgetTemplate = mustParseTemplate("metrics.html", "widget-base.html")

type metricThreshold struct {
	GreaterThanOrEqual *float64 `yaml:"gte"`
	LessThan           *float64 `yaml:"lt"`
	Color              string   `yaml:"color"`
}

type metricItem struct {
	Label      string            `yaml:"label"`
	LabelFrom  string            `yaml:"label-from"`
	Value      string            `yaml:"value"`
	Text       string            `yaml:"text"`
	TextUnit   string            `yaml:"text-unit"`
	Unit       string            `yaml:"unit"`
	Format     string            `yaml:"format"`
	Decimals   *int              `yaml:"decimals"`
	Progress   bool              `yaml:"progress"`
	Icon       string            `yaml:"icon"`
	Thresholds []metricThreshold `yaml:"thresholds"`
}

type metricView struct {
	Label   string
	Value   string
	Unit    string
	Text    string
	Icon    string
	Class   string
	Percent int
}

type metricsWidget struct {
	widgetBase        `yaml:",inline"`
	*CustomAPIRequest `yaml:",inline"`

	// 两种用法二选一：
	// 1. items: 静态声明一组指标，value 相对于整个响应 JSON 取路径
	// 2. data-path + item: 对数组里的每个元素套用同一份指标模板，动态生成卡片
	DataPath string       `yaml:"data-path"`
	Item     *metricItem  `yaml:"item"`
	Items    []metricItem `yaml:"items"`
	Columns  int          `yaml:"columns"`

	Metrics []metricView `yaml:"-"`
}

func (widget *metricsWidget) initialize() error {
	widget.withTitle("Metrics").withCacheDuration(10 * time.Second)

	if err := widget.CustomAPIRequest.initialize(); err != nil {
		return fmt.Errorf("initializing request: %v", err)
	}

	if widget.DataPath != "" && widget.Item == nil {
		return fmt.Errorf("'item' is required when 'data-path' is used")
	}

	if widget.DataPath == "" && len(widget.Items) == 0 {
		return fmt.Errorf("either 'items' or 'data-path' + 'item' is required")
	}

	if widget.Columns <= 0 {
		widget.Columns = 2
	}

	for i := range widget.Items {
		if err := validateMetricItem(&widget.Items[i]); err != nil {
			return fmt.Errorf("item %d: %v", i, err)
		}
	}

	if widget.Item != nil {
		if err := validateMetricItem(widget.Item); err != nil {
			return fmt.Errorf("item template: %v", err)
		}
	}

	return nil
}

func validateMetricItem(item *metricItem) error {
	if item.Value == "" {
		return fmt.Errorf("'value' path is required")
	}

	switch item.Format {
	case "", "text", "number", "percent", "bytes":
	default:
		return fmt.Errorf("unsupported format %q", item.Format)
	}

	return nil
}

func (widget *metricsWidget) update(ctx context.Context) {
	resp, err := fetchCustomAPIResponse(ctx, widget.CustomAPIRequest)
	if !widget.canContinueUpdateAfterHandlingErr(err) {
		return
	}

	var views []metricView

	if widget.DataPath != "" {
		node := resp.JSON.Get(widget.DataPath)
		if !node.IsArray() {
			widget.withError(fmt.Errorf("data-path %q did not resolve to a JSON array", widget.DataPath))
			return
		}

		for _, entry := range node.Result.Array() {
			views = append(views, buildMetricView(entry, *widget.Item))
		}
	} else {
		for _, item := range widget.Items {
			views = append(views, buildMetricView(resp.JSON.Result, item))
		}
	}

	widget.Metrics = views
}

func buildMetricView(source gjson.Result, item metricItem) metricView {
	label := item.Label
	if item.LabelFrom != "" {
		label = source.Get(item.LabelFrom).String()
	}

	value := source.Get(item.Value)
	view := metricView{
		Label:   label,
		Value:   formatMetricValue(value, item),
		Unit:    item.Unit,
		Icon:    item.Icon,
		Class:   metricThresholdClass(value.Float(), item.Thresholds),
		Percent: -1,
	}

	if item.Format == "percent" && item.Unit == "" {
		view.Unit = "%"
	}

	if item.Text != "" {
		text := source.Get(item.Text)
		view.Text = formatMetricValue(text, metricItem{Format: item.Format, Decimals: item.Decimals})
		if view.Text == "" {
			view.Text = text.String()
		}
		view.Text += item.TextUnit
	}

	if item.Progress {
		percent := int(math.Round(value.Float()))
		if percent < 0 {
			percent = 0
		}
		if percent > 100 {
			percent = 100
		}
		view.Percent = percent
	}

	return view
}

func formatMetricValue(value gjson.Result, item metricItem) string {
	decimals := 0
	if item.Decimals != nil {
		decimals = *item.Decimals
	}

	switch item.Format {
	case "number", "percent":
		return strconv.FormatFloat(value.Float(), 'f', decimals, 64)
	case "bytes":
		return formatMetricBytes(value.Float(), decimals)
	default:
		return value.String()
	}
}

func formatMetricBytes(bytes float64, decimals int) string {
	units := []string{"B", "KB", "MB", "GB", "TB"}
	unit := 0

	for bytes >= 1024 && unit < len(units)-1 {
		bytes /= 1024
		unit++
	}

	return strconv.FormatFloat(bytes, 'f', decimals, 64) + " " + units[unit]
}

func metricThresholdClass(number float64, thresholds []metricThreshold) string {
	for _, threshold := range thresholds {
		matched := (threshold.GreaterThanOrEqual != nil && number >= *threshold.GreaterThanOrEqual) ||
			(threshold.LessThan != nil && number < *threshold.LessThan)
		if !matched {
			continue
		}

		switch threshold.Color {
		case "negative":
			return "metric-negative"
		case "positive":
			return "metric-positive"
		default:
			return "metric-highlight"
		}
	}

	return ""
}

func (widget *metricsWidget) Render() template.HTML {
	return widget.renderTemplate(widget, metricsWidgetTemplate)
}
