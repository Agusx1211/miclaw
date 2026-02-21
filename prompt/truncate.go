package prompt

import "fmt"

func Truncate(content string, maxChars int) string {
	if maxChars <= 0 {
		panic("maxChars must be > 0")
	}
	if len(content) <= maxChars {
		return content
	}

	omitted := len(content) - maxChars
	marker := fmt.Sprintf("\n...[truncated %d chars]...\n", omitted)
	if len(marker) >= maxChars {
		return marker[:maxChars]
	}

	head := maxChars * 7 / 10
	tail := maxChars * 2 / 10
	spare := maxChars - len(marker) - head - tail
	if spare < 0 {
		available := maxChars - len(marker)
		total := head + tail
		head = int(float64(head) * float64(available) / float64(total))
		tail = int(float64(tail) * float64(available) / float64(total))
		spare = maxChars - len(marker) - head - tail
	}
	head += spare

	if head > len(content) {
		head = len(content)
	}
	if tail > len(content)-head {
		tail = len(content) - head
	}

	truncated := content[:head] + marker + content[len(content)-tail:]
	if len(truncated) > maxChars {
		panic("truncated output exceeds maxChars")
	}
	if len(truncated) < maxChars {
		panic("truncated output below maxChars")
	}
	return truncated
}
