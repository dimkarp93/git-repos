package app

import (
	"flag"
	"os"
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
	if opts.asJSON {
		if err := writeJSON(os.Stdout, filterReport(report, opts.filters)); err != nil {
			return fail(err)
		}
	} else {
		printReport(s.printer, report, opts.all, opts.filters)
	}
	if report.diverges() {
		return ExitDiff
	}
	return ExitOK
}
