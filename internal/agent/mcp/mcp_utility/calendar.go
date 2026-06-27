package mcp_utility

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"
)

const (
	defaultCalendarRangeLimit = 120
	maxCalendarRangeLimit     = 400
)

type calendarInput struct {
	Operation    string   `json:"operation"`
	Timezone     string   `json:"timezone"`
	Date         string   `json:"date"`
	StartDate    string   `json:"start_date"`
	EndDate      string   `json:"end_date"`
	Amount       int      `json:"amount"`
	Unit         string   `json:"unit"`
	Weekday      string   `json:"weekday"`
	Nth          int      `json:"nth"`
	Year         int      `json:"year"`
	Month        int      `json:"month"`
	Period       string   `json:"period"`
	FirstWeekday string   `json:"first_weekday"`
	Weekend      []string `json:"weekend"`
	Holidays     []string `json:"holidays"`
	IncludeStart *bool    `json:"include_start"`
	IncludeEnd   *bool    `json:"include_end"`
	BusinessOnly bool     `json:"business_only"`
	Limit        int      `json:"limit"`
}

func RunCalendar(ctx context.Context, args any) (map[string]any, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	var input calendarInput
	if err := mapToolArgs(args, &input); err != nil {
		return nil, err
	}
	location, err := calendarLocation(input.Timezone)
	if err != nil {
		return nil, err
	}
	operation := strings.ToLower(strings.TrimSpace(input.Operation))
	if operation == "" {
		operation = "info"
	}
	output := map[string]any{
		"ok":           true,
		"operation":    operation,
		"timezone":     location.String(),
		"result_count": 1,
	}
	switch operation {
	case "info":
		result, err := runCalendarInfo(input, location)
		if err != nil {
			return nil, err
		}
		output["result"] = result
	case "add":
		result, err := runCalendarAdd(input, location)
		if err != nil {
			return nil, err
		}
		output["result"] = result
	case "diff":
		result, err := runCalendarDiff(input, location)
		if err != nil {
			return nil, err
		}
		output["result"] = result
	case "business_add":
		result, err := runCalendarBusinessAdd(input, location)
		if err != nil {
			return nil, err
		}
		output["result"] = result
	case "business_diff":
		result, err := runCalendarBusinessDiff(input, location)
		if err != nil {
			return nil, err
		}
		output["result"] = result
	case "nth_weekday":
		result, err := runCalendarNthWeekday(input, location)
		if err != nil {
			return nil, err
		}
		output["result"] = result
	case "period_bounds":
		result, err := runCalendarPeriodBounds(input, location)
		if err != nil {
			return nil, err
		}
		output["result"] = result
	case "range":
		results, err := runCalendarRange(input, location)
		if err != nil {
			return nil, err
		}
		output["results"] = results
		output["result_count"] = len(results)
	default:
		return nil, fmt.Errorf("unsupported calendar operation %q", operation)
	}
	return output, nil
}

func calendarLocation(value string) (*time.Location, error) {
	timezone := strings.TrimSpace(value)
	if timezone == "" {
		return time.Local, nil
	}
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return nil, fmt.Errorf("invalid timezone %q", timezone)
	}
	return location, nil
}

func runCalendarInfo(input calendarInput, location *time.Location) (map[string]any, error) {
	var (
		value   time.Time
		hasTime bool
		err     error
	)
	if strings.TrimSpace(input.Date) == "" {
		value = time.Now().In(location)
		hasTime = true
	} else {
		value, hasTime, err = parseCalendarTime(input.Date, location)
		if err != nil {
			return nil, err
		}
	}
	return calendarDateInfo(value, hasTime), nil
}

func runCalendarAdd(input calendarInput, location *time.Location) (map[string]any, error) {
	value, hasTime, err := parseRequiredCalendarTime(input.Date, location, "date")
	if err != nil {
		return nil, err
	}
	unit := normalizeCalendarUnit(input.Unit)
	if unit == "" {
		unit = "day"
	}
	result, err := addCalendarUnit(value, input.Amount, unit)
	if err != nil {
		return nil, err
	}
	payload := calendarDateInfo(result, hasTime || unitHasTime(unit))
	payload["input_date"] = calendarDateInfo(value, hasTime)
	payload["amount"] = input.Amount
	payload["unit"] = unit
	return payload, nil
}

func runCalendarDiff(input calendarInput, location *time.Location) (map[string]any, error) {
	start, startHasTime, err := parseRequiredCalendarTime(input.StartDate, location, "start_date")
	if err != nil {
		return nil, err
	}
	end, endHasTime, err := parseRequiredCalendarTime(input.EndDate, location, "end_date")
	if err != nil {
		return nil, err
	}
	unit := normalizeCalendarUnit(input.Unit)
	if unit == "" {
		unit = "day"
	}
	days := civilDayNumber(end) - civilDayNumber(start)
	seconds := end.Sub(start).Seconds()
	delta := calendarDelta(start, end)
	result := map[string]any{
		"start":           calendarDateInfo(start, startHasTime),
		"end":             calendarDateInfo(end, endHasTime),
		"unit":            unit,
		"value":           diffValueForUnit(days, seconds, delta, unit),
		"days":            days,
		"weeks":           float64(days) / 7,
		"hours":           seconds / 3600,
		"minutes":         seconds / 60,
		"seconds":         seconds,
		"calendar_years":  delta.Years,
		"calendar_months": delta.Months,
		"calendar_days":   delta.Days,
		"total_months":    delta.Sign * (int(math.Abs(float64(delta.Years)))*12 + int(math.Abs(float64(delta.Months)))),
	}
	return result, nil
}

func runCalendarBusinessAdd(input calendarInput, location *time.Location) (map[string]any, error) {
	value, hasTime, err := parseRequiredCalendarTime(input.Date, location, "date")
	if err != nil {
		return nil, err
	}
	calendar, err := newBusinessCalendar(input, location)
	if err != nil {
		return nil, err
	}
	result := value
	step := 1
	if input.Amount < 0 {
		step = -1
	}
	for remaining := absInt(input.Amount); remaining > 0; {
		result = result.AddDate(0, 0, step)
		if calendar.isBusinessDay(result) {
			remaining--
		}
	}
	payload := calendarDateInfo(result, hasTime)
	payload["input_date"] = calendarDateInfo(value, hasTime)
	payload["amount"] = input.Amount
	payload["business_day"] = calendar.isBusinessDay(result)
	payload["weekend"] = calendar.weekendNames()
	payload["holiday_count"] = len(calendar.holidays)
	return payload, nil
}

func runCalendarBusinessDiff(input calendarInput, location *time.Location) (map[string]any, error) {
	start, _, err := parseRequiredCalendarTime(input.StartDate, location, "start_date")
	if err != nil {
		return nil, err
	}
	end, _, err := parseRequiredCalendarTime(input.EndDate, location, "end_date")
	if err != nil {
		return nil, err
	}
	includeStart := boolDefault(input.IncludeStart, true)
	includeEnd := boolDefault(input.IncludeEnd, true)
	calendar, err := newBusinessCalendar(input, location)
	if err != nil {
		return nil, err
	}
	sign := 1
	from := stripClock(start)
	to := stripClock(end)
	if from.After(to) {
		sign = -1
		from, to = to, from
		includeStart, includeEnd = includeEnd, includeStart
	}
	count := 0
	for current := from; !current.After(to); current = current.AddDate(0, 0, 1) {
		if current.Equal(from) && !includeStart {
			continue
		}
		if current.Equal(to) && !includeEnd {
			continue
		}
		if calendar.isBusinessDay(current) {
			count++
		}
	}
	return map[string]any{
		"start":         calendarDateInfo(start, false),
		"end":           calendarDateInfo(end, false),
		"business_days": sign * count,
		"include_start": boolDefault(input.IncludeStart, true),
		"include_end":   boolDefault(input.IncludeEnd, true),
		"weekend":       calendar.weekendNames(),
		"holiday_count": len(calendar.holidays),
	}, nil
}

func runCalendarNthWeekday(input calendarInput, location *time.Location) (map[string]any, error) {
	year, month := input.Year, input.Month
	if year == 0 || month == 0 {
		if strings.TrimSpace(input.Date) != "" {
			value, _, err := parseCalendarTime(input.Date, location)
			if err != nil {
				return nil, err
			}
			if year == 0 {
				year = value.Year()
			}
			if month == 0 {
				month = int(value.Month())
			}
		}
	}
	if year == 0 {
		return nil, fmt.Errorf("year is required")
	}
	if month < 1 || month > 12 {
		return nil, fmt.Errorf("month must be between 1 and 12")
	}
	if input.Nth == 0 {
		return nil, fmt.Errorf("nth is required and cannot be zero")
	}
	weekday, err := parseCalendarWeekday(input.Weekday)
	if err != nil {
		return nil, err
	}
	var result time.Time
	if input.Nth > 0 {
		result = time.Date(year, time.Month(month), 1, 0, 0, 0, 0, location)
		offset := (int(weekday) - int(result.Weekday()) + 7) % 7
		result = result.AddDate(0, 0, offset+(input.Nth-1)*7)
	} else {
		result = time.Date(year, time.Month(month)+1, 0, 0, 0, 0, 0, location)
		offset := (int(result.Weekday()) - int(weekday) + 7) % 7
		result = result.AddDate(0, 0, -offset+(input.Nth+1)*7)
	}
	if result.Month() != time.Month(month) {
		return nil, fmt.Errorf("the requested nth weekday does not exist in that month")
	}
	payload := calendarDateInfo(result, false)
	payload["nth"] = input.Nth
	payload["target_weekday"] = weekday.String()
	return payload, nil
}

func runCalendarPeriodBounds(input calendarInput, location *time.Location) (map[string]any, error) {
	value, _, err := parseRequiredCalendarTime(input.Date, location, "date")
	if err != nil {
		return nil, err
	}
	period := strings.ToLower(strings.TrimSpace(input.Period))
	if period == "" {
		period = strings.ToLower(strings.TrimSpace(input.Unit))
	}
	if period == "" {
		period = "month"
	}
	firstWeekday := time.Monday
	if strings.TrimSpace(input.FirstWeekday) != "" {
		firstWeekday, err = parseCalendarWeekday(input.FirstWeekday)
		if err != nil {
			return nil, err
		}
	}
	start, end, err := calendarPeriodBounds(value, period, firstWeekday)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"date":          calendarDateInfo(value, false),
		"period":        period,
		"first_weekday": firstWeekday.String(),
		"start":         calendarDateInfo(start, false),
		"end":           calendarDateInfo(end, false),
		"days":          civilDayNumber(end) - civilDayNumber(start) + 1,
	}, nil
}

func runCalendarRange(input calendarInput, location *time.Location) ([]map[string]any, error) {
	start, _, err := parseRequiredCalendarTime(input.StartDate, location, "start_date")
	if err != nil {
		return nil, err
	}
	end, _, err := parseRequiredCalendarTime(input.EndDate, location, "end_date")
	if err != nil {
		return nil, err
	}
	unit := normalizeCalendarUnit(input.Unit)
	if unit == "" {
		unit = "day"
	}
	limit := input.Limit
	if limit <= 0 {
		limit = defaultCalendarRangeLimit
	}
	if limit > maxCalendarRangeLimit {
		limit = maxCalendarRangeLimit
	}
	calendar, err := newBusinessCalendar(input, location)
	if err != nil {
		return nil, err
	}
	direction := 1
	if stripClock(start).After(stripClock(end)) {
		direction = -1
	}
	results := []map[string]any{}
	for current := stripClock(start); len(results) < limit; {
		if direction > 0 && current.After(stripClock(end)) {
			break
		}
		if direction < 0 && current.Before(stripClock(end)) {
			break
		}
		if !input.BusinessOnly || calendar.isBusinessDay(current) {
			results = append(results, calendarDateInfo(current, false))
		}
		next, err := addCalendarUnit(current, direction, unit)
		if err != nil {
			return nil, err
		}
		if next.Equal(current) {
			break
		}
		current = stripClock(next)
	}
	return results, nil
}

func parseRequiredCalendarTime(value string, location *time.Location, field string) (time.Time, bool, error) {
	if strings.TrimSpace(value) == "" {
		return time.Time{}, false, fmt.Errorf("%s is required", field)
	}
	return parseCalendarTime(value, location)
}

func parseCalendarTime(value string, location *time.Location) (time.Time, bool, error) {
	text := strings.TrimSpace(value)
	if text == "" {
		return time.Time{}, false, fmt.Errorf("date is required")
	}
	if parsed, err := time.Parse(time.RFC3339Nano, text); err == nil {
		return parsed.In(location), true, nil
	}
	layouts := []struct {
		layout  string
		hasTime bool
	}{
		{"2006-01-02 15:04:05", true},
		{"2006-01-02 15:04", true},
		{"2006-01-02T15:04:05", true},
		{"2006-01-02T15:04", true},
		{"2006/01/02 15:04:05", true},
		{"2006/01/02 15:04", true},
		{"2006-01-02", false},
		{"2006/01/02", false},
	}
	for _, candidate := range layouts {
		if parsed, err := time.ParseInLocation(candidate.layout, text, location); err == nil {
			return parsed, candidate.hasTime, nil
		}
	}
	return time.Time{}, false, fmt.Errorf("invalid date %q", value)
}

func calendarDateInfo(value time.Time, hasTime bool) map[string]any {
	isoYear, isoWeek := value.ISOWeek()
	year, month, day := value.Date()
	weekday := value.Weekday()
	result := map[string]any{
		"date":           value.Format("2006-01-02"),
		"weekday":        weekday.String(),
		"weekday_number": isoWeekdayNumber(weekday),
		"iso_year":       isoYear,
		"iso_week":       isoWeek,
		"year":           year,
		"month":          int(month),
		"day":            day,
		"day_of_year":    value.YearDay(),
		"quarter":        (int(month)-1)/3 + 1,
		"days_in_month":  daysInMonth(year, month, value.Location()),
	}
	if hasTime {
		zoneName, zoneOffset := value.Zone()
		result["rfc3339"] = value.Format(time.RFC3339)
		result["time"] = value.Format("15:04:05")
		result["zone_name"] = zoneName
		result["utc_offset_seconds"] = zoneOffset
		result["unix"] = value.Unix()
		_, offset := value.Zone()
		result["utc_offset"] = formatUTCOffset(offset)
	}
	return result
}

func normalizeCalendarUnit(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "date":
		return ""
	case "s", "sec", "second", "seconds":
		return "second"
	case "m", "min", "minute", "minutes":
		return "minute"
	case "h", "hr", "hour", "hours":
		return "hour"
	case "d", "day", "days":
		return "day"
	case "w", "week", "weeks":
		return "week"
	case "mo", "month", "months":
		return "month"
	case "q", "quarter", "quarters":
		return "quarter"
	case "y", "yr", "year", "years":
		return "year"
	default:
		return ""
	}
}

func addCalendarUnit(value time.Time, amount int, unit string) (time.Time, error) {
	switch unit {
	case "second":
		return value.Add(time.Duration(amount) * time.Second), nil
	case "minute":
		return value.Add(time.Duration(amount) * time.Minute), nil
	case "hour":
		return value.Add(time.Duration(amount) * time.Hour), nil
	case "day":
		return value.AddDate(0, 0, amount), nil
	case "week":
		return value.AddDate(0, 0, amount*7), nil
	case "month":
		return value.AddDate(0, amount, 0), nil
	case "quarter":
		return value.AddDate(0, amount*3, 0), nil
	case "year":
		return value.AddDate(amount, 0, 0), nil
	default:
		return time.Time{}, fmt.Errorf("unsupported unit %q", unit)
	}
}

func unitHasTime(unit string) bool {
	return unit == "second" || unit == "minute" || unit == "hour"
}

type calendarDeltaResult struct {
	Sign   int
	Years  int
	Months int
	Days   int
}

func calendarDelta(start time.Time, end time.Time) calendarDeltaResult {
	sign := 1
	from, to := stripClock(start), stripClock(end)
	if from.After(to) {
		sign = -1
		from, to = to, from
	}
	years := to.Year() - from.Year()
	if from.AddDate(years, 0, 0).After(to) {
		years--
	}
	base := from.AddDate(years, 0, 0)
	months := int(to.Month()) - int(base.Month())
	if months < 0 {
		months += 12
	}
	for base.AddDate(0, months, 0).After(to) {
		months--
	}
	for !base.AddDate(0, months+1, 0).After(to) {
		months++
	}
	base = base.AddDate(0, months, 0)
	days := civilDayNumber(to) - civilDayNumber(base)
	return calendarDeltaResult{
		Sign:   sign,
		Years:  sign * years,
		Months: sign * months,
		Days:   sign * days,
	}
}

func diffValueForUnit(days int, seconds float64, delta calendarDeltaResult, unit string) any {
	switch unit {
	case "second":
		return seconds
	case "minute":
		return seconds / 60
	case "hour":
		return seconds / 3600
	case "day":
		return days
	case "week":
		return float64(days) / 7
	case "month":
		return delta.Years*12 + delta.Months
	case "quarter":
		return float64(delta.Years*12+delta.Months) / 3
	case "year":
		return delta.Years
	default:
		return days
	}
}

func calendarPeriodBounds(value time.Time, period string, firstWeekday time.Weekday) (time.Time, time.Time, error) {
	year, month, _ := value.Date()
	switch strings.ToLower(strings.TrimSpace(period)) {
	case "week":
		date := stripClock(value)
		offset := (int(date.Weekday()) - int(firstWeekday) + 7) % 7
		start := date.AddDate(0, 0, -offset)
		return start, start.AddDate(0, 0, 6), nil
	case "month":
		start := time.Date(year, month, 1, 0, 0, 0, 0, value.Location())
		return start, time.Date(year, month+1, 0, 0, 0, 0, 0, value.Location()), nil
	case "quarter":
		quarterStartMonth := time.Month(((int(month)-1)/3)*3 + 1)
		start := time.Date(year, quarterStartMonth, 1, 0, 0, 0, 0, value.Location())
		return start, time.Date(year, quarterStartMonth+3, 0, 0, 0, 0, 0, value.Location()), nil
	case "year":
		start := time.Date(year, 1, 1, 0, 0, 0, 0, value.Location())
		return start, time.Date(year, 12, 31, 0, 0, 0, 0, value.Location()), nil
	default:
		return time.Time{}, time.Time{}, fmt.Errorf("unsupported period %q", period)
	}
}

type businessCalendar struct {
	weekend  map[time.Weekday]bool
	holidays map[string]bool
}

func newBusinessCalendar(input calendarInput, location *time.Location) (businessCalendar, error) {
	weekend := map[time.Weekday]bool{}
	if len(input.Weekend) == 0 {
		weekend[time.Saturday] = true
		weekend[time.Sunday] = true
	} else {
		for _, value := range input.Weekend {
			weekday, err := parseCalendarWeekday(value)
			if err != nil {
				return businessCalendar{}, err
			}
			weekend[weekday] = true
		}
	}
	holidays := map[string]bool{}
	for _, value := range input.Holidays {
		date, _, err := parseCalendarTime(value, location)
		if err != nil {
			return businessCalendar{}, fmt.Errorf("invalid holiday %q", value)
		}
		holidays[stripClock(date).Format("2006-01-02")] = true
	}
	return businessCalendar{weekend: weekend, holidays: holidays}, nil
}

func (c businessCalendar) isBusinessDay(value time.Time) bool {
	date := stripClock(value)
	if c.weekend[date.Weekday()] {
		return false
	}
	return !c.holidays[date.Format("2006-01-02")]
}

func (c businessCalendar) weekendNames() []string {
	names := []string{}
	for weekday := time.Sunday; weekday <= time.Saturday; weekday++ {
		if c.weekend[weekday] {
			names = append(names, weekday.String())
		}
	}
	return names
}

func parseCalendarWeekday(value string) (time.Weekday, error) {
	text := strings.ToLower(strings.TrimSpace(value))
	switch text {
	case "0", "7", "sun", "sunday":
		return time.Sunday, nil
	case "1", "mon", "monday":
		return time.Monday, nil
	case "2", "tue", "tues", "tuesday":
		return time.Tuesday, nil
	case "3", "wed", "wednesday":
		return time.Wednesday, nil
	case "4", "thu", "thur", "thurs", "thursday":
		return time.Thursday, nil
	case "5", "fri", "friday":
		return time.Friday, nil
	case "6", "sat", "saturday":
		return time.Saturday, nil
	default:
		return time.Sunday, fmt.Errorf("invalid weekday %q", value)
	}
}

func stripClock(value time.Time) time.Time {
	year, month, day := value.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, value.Location())
}

func civilDayNumber(value time.Time) int {
	date := stripClock(value)
	utc := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.UTC)
	return int(utc.Unix() / 86400)
}

func isoWeekdayNumber(weekday time.Weekday) int {
	if weekday == time.Sunday {
		return 7
	}
	return int(weekday)
}

func daysInMonth(year int, month time.Month, location *time.Location) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, location).Day()
}

func formatUTCOffset(seconds int) string {
	sign := "+"
	if seconds < 0 {
		sign = "-"
		seconds = -seconds
	}
	hours := seconds / 3600
	minutes := (seconds % 3600) / 60
	return fmt.Sprintf("%s%02d:%02d", sign, hours, minutes)
}

func boolDefault(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
