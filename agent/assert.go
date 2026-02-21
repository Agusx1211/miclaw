package agent

func must(ok bool, msg string) {
	if msg == "" {
		panic("assertion message must not be empty")
	}
	if !ok {
		panic(msg)
	}
}
