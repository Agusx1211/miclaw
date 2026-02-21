package tools

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

const cronSearchLimit = 60 * 24 * 366 * 4

type CronExpr struct {
	minute map[int]struct{}
	hour   map[int]struct{}
	dom    map[int]struct{}
	month  map[int]struct{}
	dow    map[int]struct{}
}

func ParseCronExpr(expr string) (CronExpr, error) {
	parts := strings.Fields(expr)
	if len(parts) != 5 {
		return CronExpr{}, fmt.Errorf("invalid cron expression: expected 5 fields")
	}
	minute, err := parseCronField(parts[0], 0, 59)
	if err != nil {
		return CronExpr{}, err
	}
	hour, err := parseCronField(parts[1], 0, 23)
	if err != nil {
		return CronExpr{}, err
	}
	dom, err := parseCronField(parts[2], 1, 31)
	if err != nil {
		return CronExpr{}, err
	}
	month, err := parseCronField(parts[3], 1, 12)
	if err != nil {
		return CronExpr{}, err
	}
	dow, err := parseCronField(parts[4], 0, 6)
	if err != nil {
		return CronExpr{}, err
	}

	return CronExpr{
		minute: setFromValues(minute),
		hour:   setFromValues(hour),
		dom:    setFromValues(dom),
		month:  setFromValues(month),
		dow:    setFromValues(dow),
	}, nil
}

func (c CronExpr) Matches(t time.Time) bool {
	t = t.UTC()
	if _, ok := c.minute[t.Minute()]; !ok {
		return false
	}
	if _, ok := c.hour[t.Hour()]; !ok {
		return false
	}
	if _, ok := c.dom[t.Day()]; !ok {
		return false
	}
	if _, ok := c.month[int(t.Month())]; !ok {
		return false
	}
	if _, ok := c.dow[int(t.Weekday())]; !ok {
		return false
	}
	return true
}

func (c CronExpr) NextAfter(t time.Time) time.Time {
	next := t.UTC().Truncate(time.Minute).Add(time.Minute)
	for i := 0; i < cronSearchLimit; i++ {
		if c.Matches(next) {
			return next
		}
		next = next.Add(time.Minute)
	}
	return next
}

func parseCronField(raw string, min, max int) ([]int, error) {
	parts := strings.Split(raw, ",")
	out := make(map[int]struct{}, max-min+1)
	for _, part := range parts {
		if part == "" {
			return nil, fmt.Errorf("invalid cron field")
		}
		if err := addCronField(part, min, max, out); err != nil {
			return nil, err
		}
	}
	values := make([]int, 0, len(out))
	for v := range out {
		values = append(values, v)
	}
	sort.Ints(values)
	return values, nil
}

func addCronField(raw string, min, max int, out map[int]struct{}) error {
	if raw == "*" {
		for i := min; i <= max; i++ {
			out[i] = struct{}{}
		}
		return nil
	}
	if strings.Contains(raw, "/") {
		return addCronStep(raw, min, max, out)
	}
	if strings.Contains(raw, "-") {
		lo, hi, err := parseCronRange(raw, min, max)
		if err != nil {
			return err
		}
		for i := lo; i <= hi; i++ {
			out[i] = struct{}{}
		}
		return nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fmt.Errorf("invalid cron field value")
	}
	if value < min || value > max {
		return fmt.Errorf("invalid cron field value")
	}
	out[value] = struct{}{}
	return nil
}

func addCronStep(raw string, min, max int, out map[int]struct{}) error {
	parts := strings.Split(raw, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return fmt.Errorf("invalid cron field")
	}
	step, err := strconv.Atoi(parts[1])
	if err != nil || step <= 0 {
		return fmt.Errorf("invalid cron step")
	}
	start := min
	end := max
	if parts[0] != "*" {
		if strings.Contains(parts[0], "-") {
			lo, hi, err := parseCronRange(parts[0], min, max)
			if err != nil {
				return err
			}
			start, end = lo, hi
		} else {
			rawValue, err := strconv.Atoi(parts[0])
			if err != nil {
				return fmt.Errorf("invalid cron field value")
			}
			if rawValue < min || rawValue > max {
				return fmt.Errorf("invalid cron field value")
			}
			start = rawValue
			end = rawValue
		}
	}
	for i := start; i <= end; i += step {
		out[i] = struct{}{}
	}
	return nil
}

func parseCronRange(raw string, min, max int) (int, int, error) {
	parts := strings.Split(raw, "-")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return 0, 0, fmt.Errorf("invalid cron range")
	}
	lo, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, fmt.Errorf("invalid cron range")
	}
	hi, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, fmt.Errorf("invalid cron range")
	}
	if lo < min || lo > max || hi < min || hi > max {
		return 0, 0, fmt.Errorf("invalid cron range")
	}
	if lo > hi {
		return 0, 0, fmt.Errorf("invalid cron range")
	}
	return lo, hi, nil
}

func setFromValues(values []int) map[int]struct{} {
	out := make(map[int]struct{}, len(values))
	for _, value := range values {
		out[value] = struct{}{}
	}
	return out
}
