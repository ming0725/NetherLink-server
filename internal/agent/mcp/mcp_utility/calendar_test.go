package mcp_utility

import (
	"context"
	"testing"
)

func TestRunCalendarAddsAndDiffsDates(t *testing.T) {
	output, err := RunCalendar(context.Background(), map[string]any{
		"operation": "add",
		"date":      "2026-06-27",
		"amount":    -10,
		"unit":      "day",
		"timezone":  "Asia/Shanghai",
	})
	if err != nil {
		t.Fatalf("RunCalendar(add) error = %v", err)
	}
	result := output["result"].(map[string]any)
	if result["date"] != "2026-06-17" || result["weekday"] != "Wednesday" {
		t.Fatalf("result = %#v, want 2026-06-17 Wednesday", result)
	}

	output, err = RunCalendar(context.Background(), map[string]any{
		"operation":  "diff",
		"start_date": "2026-01-31",
		"end_date":   "2026-03-02",
		"unit":       "day",
	})
	if err != nil {
		t.Fatalf("RunCalendar(diff) error = %v", err)
	}
	result = output["result"].(map[string]any)
	if result["days"] != 30 {
		t.Fatalf("days = %#v, want 30", result["days"])
	}
	if result["calendar_months"] != 0 || result["calendar_days"] != 30 {
		t.Fatalf("calendar delta = %#v months %#v days, want 0 months 30 days", result["calendar_months"], result["calendar_days"])
	}
}

func TestRunCalendarBusinessDayOperations(t *testing.T) {
	output, err := RunCalendar(context.Background(), map[string]any{
		"operation": "business_add",
		"date":      "2026-06-26",
		"amount":    1,
	})
	if err != nil {
		t.Fatalf("RunCalendar(business_add) error = %v", err)
	}
	result := output["result"].(map[string]any)
	if result["date"] != "2026-06-29" || result["weekday"] != "Monday" {
		t.Fatalf("result = %#v, want next Monday", result)
	}

	output, err = RunCalendar(context.Background(), map[string]any{
		"operation":  "business_diff",
		"start_date": "2026-06-26",
		"end_date":   "2026-06-30",
		"holidays":   []string{"2026-06-29"},
	})
	if err != nil {
		t.Fatalf("RunCalendar(business_diff) error = %v", err)
	}
	result = output["result"].(map[string]any)
	if result["business_days"] != 2 {
		t.Fatalf("business_days = %#v, want 2", result["business_days"])
	}
}

func TestRunCalendarNthWeekdayAndPeriodBounds(t *testing.T) {
	output, err := RunCalendar(context.Background(), map[string]any{
		"operation": "nth_weekday",
		"year":      2026,
		"month":     6,
		"weekday":   "Friday",
		"nth":       -1,
	})
	if err != nil {
		t.Fatalf("RunCalendar(nth_weekday) error = %v", err)
	}
	result := output["result"].(map[string]any)
	if result["date"] != "2026-06-26" {
		t.Fatalf("result = %#v, want 2026-06-26", result)
	}

	output, err = RunCalendar(context.Background(), map[string]any{
		"operation": "period_bounds",
		"date":      "2026-06-27",
		"period":    "quarter",
	})
	if err != nil {
		t.Fatalf("RunCalendar(period_bounds) error = %v", err)
	}
	result = output["result"].(map[string]any)
	start := result["start"].(map[string]any)
	end := result["end"].(map[string]any)
	if start["date"] != "2026-04-01" || end["date"] != "2026-06-30" {
		t.Fatalf("bounds = %#v / %#v, want Q2 2026", start, end)
	}
}

func TestRunCalendarRangeCanFilterBusinessDays(t *testing.T) {
	output, err := RunCalendar(context.Background(), map[string]any{
		"operation":     "range",
		"start_date":    "2026-06-26",
		"end_date":      "2026-06-30",
		"business_only": true,
	})
	if err != nil {
		t.Fatalf("RunCalendar(range) error = %v", err)
	}
	results := output["results"].([]map[string]any)
	if len(results) != 3 || results[0]["date"] != "2026-06-26" || results[2]["date"] != "2026-06-30" {
		t.Fatalf("results = %#v, want Friday/Monday/Tuesday", results)
	}
}
