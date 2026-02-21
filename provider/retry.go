package provider

import (
	"context"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	defaultMaxRetries = 8
	backoffBase       = time.Second
	backoffCap        = 32 * time.Second
	backoffJitter     = 0.2
)

func withRetry(ctx context.Context, maxRetries int, fn func() (*http.Response, error)) (*http.Response, error) {
	must(ctx != nil, "context is nil")
	must(fn != nil, "retry function is nil")
	if maxRetries <= 0 {
		maxRetries = defaultMaxRetries
	}
	must(maxRetries > 0, "max retries must be positive")
	attempt := 0
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		r, err := fn()
		if err != nil {
			return nil, err
		}
		must(r != nil, "retry function returned nil response")
		if !isRetriableStatus(r.StatusCode) {
			return r, nil
		}
		if attempt >= maxRetries {
			return r, nil
		}
		d := retryDelay(attempt, r.Header.Get("Retry-After"))
		if r.Body != nil {
			_ = r.Body.Close()
		}
		if err := waitForRetry(ctx, d); err != nil {
			return nil, err
		}
		attempt++
	}
}

func isRetriableStatus(code int) bool {
	must(code >= 100, "status code below HTTP range")
	must(code <= 599, "status code above HTTP range")
	return code == http.StatusTooManyRequests || code == 529
}

func retryDelay(attempt int, header string) time.Duration {
	must(attempt >= 0, "attempt cannot be negative")
	must(attempt < 62, "attempt too large for shifting")
	h := strings.TrimSpace(header)
	if d, ok := parseRetryAfter(h); ok {
		return d
	}
	d := backoffBase << attempt
	if d > backoffCap {
		d = backoffCap
	}
	j := time.Duration(float64(d) * backoffJitter * rand.Float64())
	wait := d + j
	must(wait >= d, "retry delay overflow")
	must(wait > 0, "retry delay must be positive")
	return wait
}

func parseRetryAfter(v string) (time.Duration, bool) {
	must(v == strings.TrimSpace(v), "retry-after value must be trimmed")
	must(!strings.Contains(v, "\n"), "retry-after value contains newline")
	if v == "" {
		return 0, false
	}
	s, err := strconv.Atoi(v)
	if err == nil {
		if s < 0 {
			s = 0
		}
		return time.Duration(s) * time.Second, true
	}
	t, err := http.ParseTime(v)
	if err != nil {
		return 0, false
	}
	d := time.Until(t)
	if d < 0 {
		d = 0
	}
	return d, true
}

func waitForRetry(ctx context.Context, d time.Duration) error {
	must(ctx != nil, "context is nil")
	must(d >= 0, "retry delay cannot be negative")
	if d == 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			return nil
		}
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
