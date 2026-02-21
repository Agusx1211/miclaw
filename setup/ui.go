package setup

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

type ui struct {
	r   *bufio.Reader
	out io.Writer
}

func newUI(in io.Reader, out io.Writer) *ui {
	return &ui{r: bufio.NewReader(in), out: out}
}

func (u *ui) section(title string) {
	fmt.Fprintf(u.out, "\n== %s ==\n", title)
}

func (u *ui) note(msg string) {
	fmt.Fprintln(u.out, msg)
}

func (u *ui) readLine(prompt string) (string, error) {
	fmt.Fprint(u.out, prompt)
	line, err := u.r.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func (u *ui) askString(label, current string) (string, error) {
	p := fmt.Sprintf("%s: ", label)
	if current != "" {
		p = fmt.Sprintf("%s [%s]: ", label, current)
	}
	v, err := u.readLine(p)
	if err != nil {
		return "", err
	}
	if v == "" {
		return current, nil
	}
	return v, nil
}

func (u *ui) askRequiredString(label, current string) (string, error) {
	for {
		v, err := u.askString(label, current)
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(v) != "" {
			return v, nil
		}
		u.note("Value is required.")
	}
}

func (u *ui) askSecret(label string, hasValue bool) (string, bool, error) {
	p := label + ": "
	if hasValue {
		p = label + " [set]: "
	}
	v, err := u.readLine(p)
	if err != nil {
		return "", false, err
	}
	if v == "" {
		return "", false, nil
	}
	return v, true, nil
}

func (u *ui) askRequiredSecret(label, current string) (string, error) {
	for {
		v, updated, err := u.askSecret(label, current != "")
		if err != nil {
			return "", err
		}
		if updated {
			return v, nil
		}
		if current != "" {
			return current, nil
		}
		u.note("Value is required.")
	}
}

func (u *ui) askBool(label string, current bool) (bool, error) {
	def := "y"
	if !current {
		def = "n"
	}
	for {
		v, err := u.readLine(fmt.Sprintf("%s [y/n] (%s): ", label, def))
		if err != nil {
			return false, err
		}
		if v == "" {
			return current, nil
		}
		switch strings.ToLower(v) {
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		default:
			u.note("Please enter y or n.")
		}
	}
}

func (u *ui) askInt(label string, current, min int) (int, error) {
	for {
		v, err := u.readLine(fmt.Sprintf("%s [%d]: ", label, current))
		if err != nil {
			return 0, err
		}
		if v == "" {
			return current, nil
		}
		n, err := strconv.Atoi(v)
		if err == nil && n >= min {
			return n, nil
		}
		u.note(fmt.Sprintf("Enter an integer >= %d.", min))
	}
}

func (u *ui) askFloat(label string, current, min, max float64) (float64, error) {
	for {
		v, err := u.readLine(fmt.Sprintf("%s [%.2f]: ", label, current))
		if err != nil {
			return 0, err
		}
		if v == "" {
			return current, nil
		}
		n, err := strconv.ParseFloat(v, 64)
		if err == nil && n >= min && n <= max {
			return n, nil
		}
		u.note(fmt.Sprintf("Enter a number between %.2f and %.2f.", min, max))
	}
}

func (u *ui) askCSV(label string, current []string) ([]string, error) {
	joined := strings.Join(current, ",")
	v, err := u.askString(label+" (comma separated)", joined)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(v) == "" {
		return []string{}, nil
	}
	out := []string{}
	for _, part := range strings.Split(v, ",") {
		p := strings.TrimSpace(part)
		if p != "" {
			out = append(out, p)
		}
	}
	return out, nil
}

func (u *ui) chooseOne(label string, options []string, current string) (string, error) {
	u.note(label + ":")
	defaultIdx := 0
	for i, opt := range options {
		mark := " "
		if opt == current {
			mark = "*"
			defaultIdx = i
		}
		fmt.Fprintf(u.out, "  %d. [%s] %s\n", i+1, mark, opt)
	}
	if defaultIdx < 0 || defaultIdx >= len(options) {
		defaultIdx = 0
	}
	for {
		v, err := u.readLine(fmt.Sprintf("Choose number [%d]: ", defaultIdx+1))
		if err != nil {
			return "", err
		}
		if v == "" {
			return options[defaultIdx], nil
		}
		n, err := strconv.Atoi(v)
		if err == nil && n >= 1 && n <= len(options) {
			return options[n-1], nil
		}
		u.note("Invalid choice.")
	}
}
