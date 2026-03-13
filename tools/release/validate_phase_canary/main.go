package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/livekit/psrpc/examples/pubsub/pkg/release"
)

func main() {
	filePath := flag.String("file", "", "path to phase canary yaml file")
	flag.Parse()

	if *filePath == "" {
		fmt.Fprintln(os.Stderr, "missing --file")
		os.Exit(2)
	}

	content, err := os.ReadFile(*filePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read file: %v\n", err)
		os.Exit(1)
	}

	plan, err := release.ParsePhaseCanaryPlan(content)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid phase canary template: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("phase canary template valid: phase=%s owner=%s service=%s version=%s steps=%d\n",
		plan.Phase, plan.Owner, plan.Service, plan.Version, len(plan.TrafficSteps))
}
