package main

import (
	"fmt"
	"time"

	"github.com/docopt/docopt-go"
	"github.com/lectio/dropmark"
	"github.com/lectio/generator/engine"
)

type config struct {
	Hugo           bool          `docopt:"hugo"`
	DestPath       string        `docopt:"<destPath>"`
	From           bool          `docopt:"from"`
	Dropmark       bool          `docopt:"dropmark"`
	DropmarkURLs   []string      `docopt:"<url>"`
	HTTPUserAgent  string        `docopt:"--http-user-agent"`
	HTTPTimeout    time.Duration `docopt:"--http-timeout-secs"`
	SimulateScores bool          `docopt:"--simulate-scores"`
	Verbose        bool          `docopt:"-v,--verbose"`
	Summarize      bool          `docopt:"-s,--summarize"`
}

var usage = `Lectio Content Generator.

Usage:
  generate hugo <destPath> from dropmark <url>... [--http-user-agent=<agent> --http-timeout-secs=<timeout> --simulate-scores --verbose --summarize]

Options:
  -h --help                     Show this screen.
  --http-user-agent=<agent>     The string to use for HTTP User-Agent header value
  --http-timeout-secs=<timeout> How many seconds to wait before giving up on the HTTP request
  --simulate-scores             Don't call Facebook, LinkedIn, etc. APIs -- simulate the values instead
  -v --verbose                  Show verbose messages
  -s --summarize                Summarize activity after execution
  --version                     Show version.`

func main() {
	arguments, pdErr := docopt.ParseDoc(usage)
	if pdErr != nil {
		panic(pdErr)
	}
	options := new(config)
	bindErr := arguments.Bind(options)
	if bindErr != nil {
		fmt.Printf("%+v, %v", options, bindErr)
		panic(pdErr)
	}

	if options.Hugo && options.From && options.Dropmark {
		if len(options.HTTPUserAgent) == 0 {
			options.HTTPUserAgent = "github.com/lectio/generate"
		}
		if options.HTTPTimeout <= 0 {
			options.HTTPTimeout = time.Second * 90
		} else {
			options.HTTPTimeout = time.Second * options.HTTPTimeout
		}
		fmt.Printf("verbose %v, summarize: %v, simulate: %v, httpUserAgent: %s, httpTimeout: %v\n", options.Verbose, options.Summarize, options.SimulateScores, options.HTTPUserAgent, options.HTTPTimeout)

		for i := 0; i < len(options.DropmarkURLs); i++ {
			dropmarkURL := options.DropmarkURLs[i]
			collection, getErr := dropmark.GetDropmarkCollection(dropmarkURL, options.HTTPUserAgent, options.HTTPTimeout)
			if getErr != nil {
				panic(getErr)
			}
			generator := engine.NewHugoGenerator(collection, options.DestPath, options.Verbose, true)
			generator.GenerateContent()
			if options.Summarize {
				fmt.Println(generator.GetActivitySummary())
			}
		}
	}
}
