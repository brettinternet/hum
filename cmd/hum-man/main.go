package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"hum/internal/cli"
)

func main() {
	version := flag.String("version", "dev", "Hum version shown in the manual footer")
	buildTime := flag.String("build-time", "unknown", "Hum build time shown in generated help")
	date := flag.String("date", "", "manual date in YYYY-MM-DD form")
	flag.Parse()

	if *date == "" {
		fmt.Fprintln(os.Stderr, "hum-man: --date is required")
		os.Exit(2)
	}
	root := cli.NewRootCommand(*version, *buildTime, io.Discard, io.Discard)
	if err := cli.WriteManPage(os.Stdout, root, *date); err != nil {
		fmt.Fprintf(os.Stderr, "hum-man: %v\n", err)
		os.Exit(1)
	}
}
