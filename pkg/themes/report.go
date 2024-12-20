package themes

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"go/token"
	"html"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/axw/gocov"
	"github.com/rotisserie/eris"
)

// ReportOptions holds various options used when generating the final
// HTML report.
type ReportOptions struct {
	SortOrder           SortOrder
	Stylesheet          string
	CoverageFunctionMin uint8
	CoverageFunctionMax uint8
	CoveragePackageMin  uint8
	CoveragePackageMax  uint8
}

type report struct {
	ReportOptions
	packages []*gocov.Package
}

type SortOrder string

const SortOrderHighCoverage SortOrder = "high-coverage"
const SortOrderLowCoverage SortOrder = "low-coverage"
const SortOrderLocation SortOrder = "location"

func (so SortOrder) Valid() bool {
	switch so {
	case SortOrderHighCoverage, SortOrderLowCoverage, SortOrderLocation:
		return true
	default:
		return false
	}
}

func unmarshalJSON(data []byte) (packages []*gocov.Package, err error) {
	result := &struct{ Packages []*gocov.Package }{}
	err = json.Unmarshal(data, result)
	if err == nil {
		packages = result.Packages
	}
	return
}

// NewReport creates a new report.
func newReport() (r *report) {
	r = &report{}
	return
}

// AddPackage adds a package's coverage information to the report.
func (r *report) addPackage(p *gocov.Package) {
	i := sort.Search(len(r.packages), func(i int) bool {
		return r.packages[i].Name >= p.Name
	})
	if i < len(r.packages) && r.packages[i].Name == p.Name {
		r.packages[i].Accumulate(p)
	} else {
		head := r.packages[:i]
		tail := append([]*gocov.Package{p}, r.packages[i:]...)
		r.packages = append(head, tail...)
	}
}

// Clear clears the coverage information from the report.
func (r *report) clear() {
	r.packages = nil
}

func buildReportPackage(pkg *gocov.Package, r *report) reportPackage {
	rv := reportPackage{
		Pkg:       pkg,
		Functions: make(reportFunctionList, 0),
	}
	for _, fn := range pkg.Functions {
		reached := 0
		for _, stmt := range fn.Statements {
			if stmt.Reached > 0 {
				reached++
			}
		}
		rf := reportFunction{Function: fn, StatementsReached: reached}
		covp := rf.CoveragePercent()
		if covp >= float64(r.CoverageFunctionMin) && covp <= float64(r.CoverageFunctionMax) {
			rv.Functions = append(rv.Functions, rf)
		}
		rv.TotalStatements += len(fn.Statements)
		rv.ReachedStatements += reached
	}
	switch r.SortOrder {
	case SortOrderHighCoverage:
		sort.Sort(rv.Functions)
	case SortOrderLowCoverage:
		sort.Sort(sort.Reverse(rv.Functions))
	case SortOrderLocation:
		sort.Sort(locationOrderedFunctionList(rv.Functions))
	}
	return rv
}

// printReport prints a coverage report to the given writer.
func printReport(w io.Writer, r *report) error {
	data := curTheme.Data()

	// Base64 decoding of style data and script.
	s, err := base64.StdEncoding.DecodeString(data.Style)
	if err != nil {
		return eris.Wrap(err, "decode style")
	}
	css := string(s)
	// Decode the script also.
	sc, err := base64.StdEncoding.DecodeString(data.Script)
	if err != nil {
		return eris.Wrap(err, "decode script")
	}

	if len(r.Stylesheet) > 0 {
		// Inline CSS.
		f, err := os.Open(r.Stylesheet)
		if err != nil {
			return eris.Wrap(err, "print report")
		}
		style, err := io.ReadAll(f)
		if err != nil {
			return eris.Wrap(err, "read style")
		}
		css = string(style)
	}
	reportPackages := make(reportPackageList, len(r.packages))
	pkgNames := make([]string, len(r.packages))
	for i, pkg := range r.packages {
		reportPackages[i] = buildReportPackage(pkg, r)
		pkgNames[i] = pkg.Name
	}

	data.Script = string(sc)
	data.Style = css
	data.Packages = reportPackages
	data.Command = fmt.Sprintf("gocov test %s | gocov-html %s",
		strings.Join(pkgNames, " "),
		strings.Join(os.Args[1:], " "),
	)

	if len(reportPackages) > 1 {
		rv := reportPackage{
			Pkg: &gocov.Package{Name: "Report Total"},
		}
		for _, rp := range reportPackages {
			rv.ReachedStatements += rp.ReachedStatements
			rv.TotalStatements += rp.TotalStatements
		}
		data.Overview = &rv
	}
	err = curTheme.Template().Execute(w, data)
	return eris.Wrap(err, "execute template")
}

func exists(path string) (bool, error) {
	if _, err := os.Stat(path); err != nil {
		return false, err
	}
	return true, nil
}

// HTMLReportCoverage outputs an HTML report on stdout by
// parsing JSON data generated by axw/gocov. The css parameter
// is an absolute path to a custom stylesheet. Use an empty
// string to use the default stylesheet available.
func HTMLReportCoverage(r io.Reader, opts ReportOptions) error {
	t0 := time.Now()
	report := newReport()
	report.ReportOptions = opts

	// Custom stylesheet?
	stylesheet := ""
	if opts.Stylesheet != "" {
		if _, err := exists(opts.Stylesheet); err != nil {
			return eris.Wrap(err, "stylesheet")
		}
		stylesheet = opts.Stylesheet
	}
	report.Stylesheet = stylesheet

	data, err := io.ReadAll(r)
	if err != nil {
		return eris.Wrap(err, "read coverage data")
	}

	packages, err := unmarshalJSON(data)
	if err != nil {
		return eris.Wrap(err, "unmarshal coverage data")
	}

	for _, pkg := range packages {
		reachedStatements := 0
		totalStatements := 0
		for _, fn := range pkg.Functions {
			reached := 0
			for _, stmt := range fn.Statements {
				if stmt.Reached > 0 {
					reached++
				}
			}
			reachedStatements += reached
			totalStatements += len(fn.Statements)
		}

		stmtPercent := float64(reachedStatements) / float64(totalStatements) * 100

		fmt.Fprintf(os.Stderr, fmt.Sprintf("[%s] - reachedStatements: %v - totalStatements: %v - stmtPercent: %v\n", pkg.Name, reachedStatements, totalStatements, stmtPercent))
		if stmtPercent >= float64(opts.CoveragePackageMin) && stmtPercent <= float64(opts.CoveragePackageMax) {
			report.addPackage(pkg)
		}
	}
	fmt.Println()
	err = printReport(os.Stdout, report)
	fmt.Fprintf(os.Stderr, "Took %v\n", time.Since(t0))
	return eris.Wrap(err, "HTML report")
}

const (
	hitPrefix  = "    "
	missPrefix = "MISS"
)

type reportPackageList []reportPackage

type reportPackage struct {
	Pkg               *gocov.Package
	Functions         reportFunctionList
	TotalStatements   int
	ReachedStatements int
}

// PercentageReached computes the percentage of reached statements by the tests
// for a package.
func (rp *reportPackage) PercentageReached() float64 {
	var rv float64
	if rp.TotalStatements > 0 {
		rv = float64(rp.ReachedStatements) / float64(rp.TotalStatements) * 100
	}
	return rv
}

// reportFunction is a gocov Function with some added stats.
type reportFunction struct {
	*gocov.Function
	StatementsReached int
}

// functionLine holds the line of code, its line number in the source file
// and whether the tests reached it.
type functionLine struct {
	Code       string
	LineNumber int
	Missed     bool
}

// CoveragePercent is the percentage of code coverage for a function. Returns 100
// if the function has no statement.
func (f reportFunction) CoveragePercent() float64 {
	reached := f.StatementsReached
	var stmtPercent float64 = 0
	if len(f.Statements) > 0 {
		stmtPercent = float64(reached) / float64(len(f.Statements)) * 100
	} else if len(f.Statements) == 0 {
		stmtPercent = 100
	}
	return stmtPercent
}

// ShortFileName returns the base path of the function's file name. Provided for
// convenience to be used in the HTML template of the theme.
func (f reportFunction) ShortFileName() string {
	return filepath.Base(f.File)
}

// Lines returns information about all a function's Lines of code.
func (f reportFunction) Lines() []functionLine {
	type annotator struct {
		fset  *token.FileSet
		files map[string]*token.File
	}
	a := &annotator{}
	a.fset = token.NewFileSet()
	a.files = make(map[string]*token.File)

	// Load the file for line information. Probably overkill, maybe
	// just compute the lines from offsets in here.
	setContent := false
	file := a.files[f.File]
	if file == nil {
		info, err := os.Stat(f.File)
		if err != nil {
			panic(err)
		}
		file = a.fset.AddFile(f.File, a.fset.Base(), int(info.Size()))
		setContent = true
	}

	data, err := ioutil.ReadFile(f.File)
	if err != nil {
		panic(err)
	}

	if setContent {
		// This processes the content and records line number info.
		file.SetLinesForContent(data)
	}

	statements := f.Statements[:]
	lineno := file.Line(file.Pos(f.Start))
	lines := strings.Split(string(data)[f.Start:f.End], "\n")
	fls := make([]functionLine, len(lines))

	for i, line := range lines {
		lineno := lineno + i
		statementFound := false
		hit := false
		for j := 0; j < len(statements); j++ {
			start := file.Line(file.Pos(statements[j].Start))
			if start == lineno {
				statementFound = true
				if !hit && statements[j].Reached > 0 {
					hit = true
				}
				statements = append(statements[:j], statements[j+1:]...)
			}
		}
		hitmiss := hitPrefix
		if statementFound && !hit {
			hitmiss = missPrefix
		}
		fls[i] = functionLine{
			Missed:     hitmiss == missPrefix,
			LineNumber: lineno,
			Code:       html.EscapeString(strings.Replace(line, "\t", "    ", -1)),
		}
	}
	return fls
}

type locationOrderedFunctionList reportFunctionList

func (l locationOrderedFunctionList) Len() int {
	return len(l)
}

func (l locationOrderedFunctionList) Less(i, j int) bool {
	if l[i].File == l[j].File {
		return l[i].Start < l[j].Start
	}
	return l[i].File < l[j].File
}

func (l locationOrderedFunctionList) Swap(i, j int) {
	l[i], l[j] = l[j], l[i]
}

// reportFunctionList is a list of functions for a report.
type reportFunctionList []reportFunction

func (l reportFunctionList) Len() int {
	return len(l)
}

func (l reportFunctionList) Less(i, j int) bool {
	var left, right float64
	if len(l[i].Statements) > 0 {
		left = float64(l[i].StatementsReached) / float64(len(l[i].Statements))
	}
	if len(l[j].Statements) > 0 {
		right = float64(l[j].StatementsReached) / float64(len(l[j].Statements))
	}
	if left < right {
		return true
	}
	return left == right && len(l[i].Statements) < len(l[j].Statements)
}

func (l reportFunctionList) Swap(i, j int) {
	l[i], l[j] = l[j], l[i]
}
