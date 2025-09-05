package wizard

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/robocorp/rcc/common"
	"github.com/robocorp/rcc/operations"
	"github.com/robocorp/rcc/pathlib"
	"github.com/robocorp/rcc/pretty"
)

const (
	newline = '\n'
)

var (
	digitPattern = regexp.MustCompile("^\\d+$")
)

func choose(question, label string, candidates []string) (string, error) {
	keys := []string{}
	common.Stdout("%s%s:%s\n", pretty.Grey, label, pretty.Reset)
	for index, candidate := range candidates {
		key := index + 1
		keys = append(keys, fmt.Sprintf("%d", key))
		common.Stdout("  %s%2d: %s%s%s\n", pretty.Grey, key, pretty.White, candidate, pretty.Reset)
	}
	common.Stdout("\n")
	selectable := strings.Join(keys, "|")
	pattern, err := regexp.Compile(fmt.Sprintf("^(?:%s)?$", selectable))
	if err != nil {
		return "", err
	}
	reply, err := ask(question, "1", regexpValidation(pattern, "Give selections number from above list."))
	if err != nil {
		return "", err
	}
	selected, err := strconv.Atoi(reply)
	if err != nil {
		return "", err
	}
	return candidates[selected-1], nil
}

func Create(arguments []string) error {
	common.Stdout("\n")

	warning(len(arguments) > 1, "You provided more than one argument, but only the first one will be\nused as the name.")
	robotName, err := ask("Give robot name", firstOf(arguments, "my-first-robot"), regexpValidation(namePattern, "Use just normal english word characters and no spaces!"))

	if err != nil {
		return err
	}

	fullpath, err := filepath.Abs(robotName)
	if err != nil {
		return err
	}

	if pathlib.IsDir(fullpath) {
		return fmt.Errorf("Folder %s already exists. Try with other name.", robotName)
	}

	templates := operations.ListTemplatesWithDescription(false)
	descriptions := make([]string, 0, len(templates))
	lookup := make(map[string]string)
	for _, template := range templates {
		descriptions = append(descriptions, template[1])
		lookup[template[1]] = template[0]
	}
	sort.Strings(descriptions)
	selected, err := choose("Choose a template", "Templates", descriptions)
	if err != nil {
		return err
	}

	err = operations.InitializeWorkarea(fullpath, lookup[selected], false, false)
	if err != nil {
		return err
	}

	common.Stdout("%s%s%sThe %s%s%s robot has been created to: %s%s%s\n", pretty.Yellow, pretty.Sparkles, pretty.Green, pretty.Cyan, selected, pretty.Green, pretty.Cyan, robotName, pretty.Reset)
	common.Stdout("\n")

	common.Stdout("%s%sGet started with following commands:%s\n", pretty.White, pretty.Rocket, pretty.Reset)
	common.Stdout("\n")

	common.Stdout("%s$ %scd %s%s\n", pretty.Grey, pretty.Cyan, robotName, pretty.Reset)
	common.Stdout("%s$ %srcc run%s\n", pretty.Grey, pretty.Cyan, pretty.Reset)
	common.Stdout("%s# or with name in case of multiple tasks in one robot\n$ %srcc run --task \"<name of tasks>\"%s\n", pretty.Grey, pretty.Cyan, pretty.Reset)
	common.Stdout("\n")

	return nil
}
