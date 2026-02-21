package tools

func must(ok bool, msg string) {
	if msg == "" {
		panic("assert message is required")
	}
	if !ok {
		panic(msg)
	}
}
