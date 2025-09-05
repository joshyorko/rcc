package pretty

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/robocorp/rcc/common"
	"golang.org/x/term"
)

var (
	titlePattern = regexp.MustCompile("^#{1,5}\\s+")
	codePattern  = regexp.MustCompile("^ {4,}\\S+")
	blockPattern = regexp.MustCompile("^```")
)

func Page(content []byte) {
	width, height, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || !Interactive {
		common.Stdout("\n%s\n", content)
		return
	}

	titleStyle := fmt.Sprintf("%s%s", Bold, Underline)
	codeStyle := Faint

	limit := height - 3
	reader := bufio.NewReader(os.Stdin)
	lines := strings.SplitAfter(string(content), "\n")
	fmt.Printf("%s%s", Home, Clear)
	row := 0
	block := false
	for _, line := range lines {
		flat := strings.TrimRight(line, " \t\r\n")
		if blockPattern.MatchString(flat) {
			block = !block
			continue
		}
		adjust := len(flat) / width
		row += 1 + adjust
		if row > limit {
			fmt.Print("\n-- press enter to continue or ctrl-c to stop --")
			reader.ReadLine()
			fmt.Printf("%s%s", Home, Clear)
			row = 1
		}
		style := ""
		if titlePattern.MatchString(flat) {
			style = titleStyle
		}
		if block || codePattern.MatchString(flat) {
			style = codeStyle
		}
		fmt.Printf("%s%s%s\n", style, flat, Reset)
	}
}
