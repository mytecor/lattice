package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	taskcontract "github.com/mytecor/lattice/task-contract"
)

func main() {
	repoFlag := flag.String("repo", "local", "Task repository identifier")
	commitFlag := flag.String("commit", "HEAD", "Git commit hash")
	jsonFlag := flag.Bool("json", false, "Output TaskSpec as JSON")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [flags] <task-file.md>\n", os.Args[0])
		flag.PrintDefaults()
	}

	flag.Parse()

	if flag.NArg() < 1 {
		flag.Usage()
		os.Exit(2)
	}

	path := flag.Arg(0)
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading file %s: %v\n", path, err)
		os.Exit(1)
	}

	spec, err := taskcontract.ParseTask(*repoFlag, path, *commitFlag, data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "task contract validation failed: %v\n", err)
		os.Exit(1)
	}

	if *jsonFlag {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(spec); err != nil {
			fmt.Fprintf(os.Stderr, "failed to encode JSON: %v\n", err)
			os.Exit(1)
		}
		return
	}

	stateStr := "actionable"
	if !spec.IsActionable() {
		stateStr = fmt.Sprintf("terminal (%s)", spec.Result.Status)
	}

	fmt.Printf("Task %s is valid: state=%s, checks=%d\n", spec.ID, stateStr, len(spec.Checks))
}
