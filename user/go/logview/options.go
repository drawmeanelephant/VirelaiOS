package main

import (
	"sort"
	"strconv"
	"strings"

	"virelai/vi"
)

type options struct {
	filter filter
	text   bool
	follow bool
	export bool
	polls  int
}

const usage = "LOGVIEW [-text] [-follow|-snapshot] [-app A,B] [-level D|I|W|E] [-tag TAG] [-grep TEXT] [-polls N] [export]"

func parseOptions(args []string) (options, bool) {
	o := options{filter: filter{level: 'D'}, follow: true}
	modeSet := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		// The guest has seven user argv slots. Compact -key=value flags let
		// all four filters fit alongside -text, -follow and export.
		value := ""
		inline := false
		if strings.HasPrefix(arg, "-") {
			arg, value, inline = strings.Cut(arg, "=")
		}
		if inline && (arg == "-text" || arg == "-follow" || arg == "-snapshot") {
			return options{}, false
		}
		switch arg {
		case "-text":
			o.text = true
		case "-follow":
			o.follow = true
			modeSet = true
		case "-snapshot":
			o.follow = false
			modeSet = true
		case "export":
			o.export = true
			if !modeSet {
				o.follow = false
			}
		case "-app", "-level", "-tag", "-grep", "-polls":
			if !inline {
				i++
				if i == len(args) {
					return options{}, false
				}
				value = args[i]
			}
			switch arg {
			case "-app":
				o.filter.apps = strings.Split(value, ",")
				if len(o.filter.apps) > vi.MaxDirEntries {
					return options{}, false
				}
				for _, app := range o.filter.apps {
					if vi.AppLogPath(app) == "" {
						return options{}, false
					}
				}
				sort.Strings(o.filter.apps)
			case "-level":
				if len(value) != 1 || levelRank(value[0]) < 0 {
					return options{}, false
				}
				o.filter.level = value[0]
			case "-tag":
				o.filter.tag = value
			case "-grep":
				o.filter.grep = value
			case "-polls":
				n, err := strconv.Atoi(value)
				if err != nil || n < 1 {
					return options{}, false
				}
				o.polls = n
			}
		default:
			return options{}, false
		}
	}
	return o, true
}

func sortedApps(bodies map[string][]byte) []string {
	apps := make([]string, 0, len(bodies))
	for app := range bodies {
		apps = append(apps, app)
	}
	sort.Strings(apps)
	return apps
}
