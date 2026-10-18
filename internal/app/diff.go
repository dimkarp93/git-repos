package app

import (
	"flag"
	"os"

	"github.com/dimkarp93/git-repos/internal/render"
)

func diffFlags(opts *options) *flag.FlagSet {
	fs := newFlagSet("diff", opts)
	fs.BoolVar(&opts.fetch, "fetch", false, "run git fetch before comparing")
	fs.BoolVar(&opts.all, "all", false, "show repositories that are in sync too")
	fs.BoolVar(&opts.refresh, "refresh", false, "ignore cached default branches")
	fs.BoolVar(&opts.asJSON, "json", false, "print the report as JSON")
	fs.BoolVar(&opts.noProgress, "no-progress", false, "do not show the progress indicator")
	opts.filters.registerView(fs)
	opts.filters.registerName(fs)
	return fs
}

func outputReport(p *render.Printer, report Report, opts options) error {
	if opts.asJSON {
		return writeJSON(os.Stdout, filterReport(report, opts.filters))
	}

	printReport(p, report, opts.all, opts.filters)
	return nil
}

func runDiff(args []string) int {
	opts := options{}
	fs := diffFlags(&opts)
	if ok, code := parseFlags(fs, args); !ok {
		return code
	}

	s, err := start(opts)
	if err != nil {
		return fail(err)
	}
	defer s.close()

	s.pr.setPlan(4)
	s.spinner.Start(s.pr.label)
	report, err := build(s.ctx, s.prov, s.store, s.roots, s.cfg, opts, s.pr)
	s.spinner.Stop()
	if err != nil {
		return fail(err)
	}

	if err := outputReport(s.printer, report, opts); err != nil {
		return fail(err)
	}
	if report.diverges() {
		return ExitDiff
	}
	return ExitOK
}
