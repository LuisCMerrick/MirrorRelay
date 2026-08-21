package warmup

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type schedule interface {
	Next(time.Time) time.Time
}

type intervalSchedule struct{ interval time.Duration }

func (s intervalSchedule) Next(after time.Time) time.Time { return after.Add(s.interval) }

type cronField struct {
	minimum  int
	maximum  int
	allowed  []bool
	wildcard bool
}

func (f cronField) matches(value int) bool {
	return value >= f.minimum && value <= f.maximum && f.allowed[value-f.minimum]
}

type cronSchedule struct {
	minute     cronField
	hour       cronField
	dayOfMonth cronField
	month      cronField
	dayOfWeek  cronField
}

func (s cronSchedule) Next(after time.Time) time.Time {
	candidate := after.Truncate(time.Minute).Add(time.Minute)
	limit := candidate.AddDate(5, 0, 0)
	for candidate.Before(limit) {
		if s.matches(candidate) {
			return candidate
		}
		candidate = candidate.Add(time.Minute)
	}
	return time.Time{}
}

func (s cronSchedule) matches(value time.Time) bool {
	if !s.minute.matches(value.Minute()) || !s.hour.matches(value.Hour()) || !s.month.matches(int(value.Month())) {
		return false
	}
	dayOfMonth := s.dayOfMonth.matches(value.Day())
	dayOfWeek := s.dayOfWeek.matches(int(value.Weekday()))
	switch {
	case s.dayOfMonth.wildcard && s.dayOfWeek.wildcard:
		return true
	case s.dayOfMonth.wildcard:
		return dayOfWeek
	case s.dayOfWeek.wildcard:
		return dayOfMonth
	default:
		return dayOfMonth || dayOfWeek
	}
}

func ValidateSchedule(expression string) error {
	if strings.TrimSpace(expression) == "" {
		return nil
	}
	parsed, err := parseSchedule(expression)
	if err != nil {
		return err
	}
	if parsed.Next(time.Now().UTC()).IsZero() {
		return errors.New("schedule has no occurrence within five years")
	}
	return nil
}

func NextRunAt(expression string, after time.Time) (time.Time, error) {
	if strings.TrimSpace(expression) == "" {
		return time.Time{}, nil
	}
	parsed, err := parseSchedule(expression)
	if err != nil {
		return time.Time{}, err
	}
	next := parsed.Next(after)
	if next.IsZero() {
		return time.Time{}, errors.New("schedule has no occurrence within five years")
	}
	return next, nil
}

func parseSchedule(expression string) (schedule, error) {
	expression = strings.TrimSpace(strings.ToLower(expression))
	switch expression {
	case "@hourly":
		expression = "0 * * * *"
	case "@daily":
		expression = "0 0 * * *"
	}
	if strings.HasPrefix(expression, "@every ") {
		value := strings.TrimSpace(strings.TrimPrefix(expression, "@every "))
		interval, err := time.ParseDuration(value)
		if err != nil || interval < 30*time.Second {
			return nil, errors.New("@every interval must be a valid duration of at least 30s")
		}
		return intervalSchedule{interval: interval}, nil
	}
	if strings.HasPrefix(expression, "@") {
		return nil, fmt.Errorf("unsupported schedule macro %q", expression)
	}
	parts := strings.Fields(expression)
	if len(parts) != 5 {
		return nil, errors.New("cron schedule must contain exactly five fields")
	}
	minute, err := parseCronField(parts[0], 0, 59, false)
	if err != nil {
		return nil, fmt.Errorf("minute field: %w", err)
	}
	hour, err := parseCronField(parts[1], 0, 23, false)
	if err != nil {
		return nil, fmt.Errorf("hour field: %w", err)
	}
	dayOfMonth, err := parseCronField(parts[2], 1, 31, false)
	if err != nil {
		return nil, fmt.Errorf("day-of-month field: %w", err)
	}
	month, err := parseCronField(parts[3], 1, 12, false)
	if err != nil {
		return nil, fmt.Errorf("month field: %w", err)
	}
	dayOfWeek, err := parseCronField(parts[4], 0, 7, true)
	if err != nil {
		return nil, fmt.Errorf("day-of-week field: %w", err)
	}
	return cronSchedule{minute: minute, hour: hour, dayOfMonth: dayOfMonth, month: month, dayOfWeek: dayOfWeek}, nil
}

func parseCronField(value string, minimum, maximum int, normalizeSunday bool) (cronField, error) {
	fieldMaximum := maximum
	if normalizeSunday {
		fieldMaximum = 6
	}
	field := cronField{minimum: minimum, maximum: fieldMaximum, allowed: make([]bool, fieldMaximum-minimum+1)}
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			return cronField{}, errors.New("empty list item")
		}
		base := item
		step := 1
		if slash := strings.IndexByte(item, '/'); slash >= 0 {
			if strings.Count(item, "/") != 1 {
				return cronField{}, fmt.Errorf("invalid step %q", item)
			}
			base = item[:slash]
			parsedStep, err := strconv.Atoi(item[slash+1:])
			if err != nil || parsedStep <= 0 {
				return cronField{}, fmt.Errorf("invalid step %q", item[slash+1:])
			}
			step = parsedStep
		}
		start, end := minimum, maximum
		switch {
		case base == "*":
			field.wildcard = true
		case strings.Contains(base, "-"):
			bounds := strings.Split(base, "-")
			if len(bounds) != 2 {
				return cronField{}, fmt.Errorf("invalid range %q", base)
			}
			var err error
			start, err = parseCronNumber(bounds[0], minimum, maximum)
			if err != nil {
				return cronField{}, err
			}
			end, err = parseCronNumber(bounds[1], minimum, maximum)
			if err != nil {
				return cronField{}, err
			}
			if start > end {
				return cronField{}, fmt.Errorf("range start %d exceeds end %d", start, end)
			}
		default:
			parsed, err := parseCronNumber(base, minimum, maximum)
			if err != nil {
				return cronField{}, err
			}
			start = parsed
			if strings.Contains(item, "/") {
				end = maximum
			} else {
				end = parsed
			}
		}
		for current := start; current <= end; current += step {
			normalized := current
			if normalizeSunday && normalized == 7 {
				normalized = 0
			}
			field.allowed[normalized-minimum] = true
		}
	}
	for _, allowed := range field.allowed {
		if allowed {
			return field, nil
		}
	}
	return cronField{}, errors.New("field selects no values")
}

func parseCronNumber(value string, minimum, maximum int) (int, error) {
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < minimum || parsed > maximum {
		return 0, fmt.Errorf("value %q must be in %d..%d", value, minimum, maximum)
	}
	return parsed, nil
}
