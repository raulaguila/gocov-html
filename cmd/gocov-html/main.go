package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"runtime"

	"github.com/matm/gocov-html/pkg/config"
	"github.com/matm/gocov-html/pkg/themes"
)

func main() {
	log.SetFlags(0)

	css := flag.String("s", "", "path to custom CSS file")
	showVersion := flag.Bool("v", false, "show program version")
	showDefaultCSS := flag.Bool("d", false, "output CSS of default theme")
	listThemes := flag.Bool("lt", false, "list available themes")
	theme := flag.String("t", "golang", "theme to use for rendering")
	reverseOrder := flag.Bool("r", false, "put lower coverage functions on top")
	sortOrder := flag.String("sort", "high-coverage", "sort functions by high-coverage, low-coverage or location")
	maxFunctionCoverage := flag.Uint64("fmax", 100, "only show functions whose coverage is greater than fmax")
	minFunctionCoverage := flag.Uint64("fmin", 0, "only show functions whose coverage is smaller than fmin")
	maxPackageCoverage := flag.Uint64("pmax", 100, "only show packages whose coverage is greater than pmax")
	minPackageCoverage := flag.Uint64("pmin", 0, "only show packages whose coverage is smaller than pmin")

	flag.Parse()

	if *showVersion {
		fmt.Printf("Gocov-HTML Version:      %s\n", config.Version)
		fmt.Printf("Go version:   %s\n", runtime.Version())
		fmt.Printf("OS/Arch:      %s/%s\n", runtime.GOOS, runtime.GOARCH)
		return
	}

	if *listThemes {
		for _, th := range themes.List() {
			fmt.Printf("%-10s -- %s\n", th.Name(), th.Description())
		}
		return
	}

	if *minFunctionCoverage > *maxFunctionCoverage {
		log.Fatal("error: empty report if cmin > cmax, please use a smaller cmin value.")
	}
	if *maxFunctionCoverage > 100 {
		*maxFunctionCoverage = 100
	}
	if *minFunctionCoverage < 0 {
		*minFunctionCoverage = 0
	}
	if *maxPackageCoverage > 100 {
		*maxPackageCoverage = 100
	}
	if *minPackageCoverage < 0 {
		*minPackageCoverage = 0
	}

	if err := themes.Use(*theme); err != nil {
		log.Fatalf("Theme selection: %v", err)
	}

	if *showDefaultCSS {
		fmt.Println(themes.Current().Data().Style)
		return
	}

	var r io.Reader
	switch flag.NArg() {
	case 0:
		r = os.Stdin
	case 1:
		var err error
		if r, err = os.Open(flag.Arg(0)); err != nil {
			log.Fatal(err)
		}
	default:
		log.Fatalf("Usage: %s data.json\n", os.Args[0])
	}

	var sortOrderOpt = themes.SortOrder(*sortOrder)
	if !sortOrderOpt.Valid() {
		log.Fatalf("Invalid sort order: %q\n", sortOrderOpt)
	}

	if *reverseOrder {
		sortOrderOpt = themes.SortOrderLowCoverage
	}

	opts := themes.ReportOptions{
		SortOrder:           sortOrderOpt,
		Stylesheet:          *css,
		CoverageFunctionMin: uint8(*minFunctionCoverage),
		CoverageFunctionMax: uint8(*maxFunctionCoverage),
		CoveragePackageMin:  uint8(*minPackageCoverage),
		CoveragePackageMax:  uint8(*maxPackageCoverage),
	}
	if err := themes.HTMLReportCoverage(r, opts); err != nil {
		log.Fatal(err)
	}
}
