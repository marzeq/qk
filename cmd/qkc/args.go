package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Args struct {
	baseDir     string
	excludeDirs []string
	output      string
	verbose     bool
	debug       bool
}

type ArgParser struct {
	Args []string
	Pos  int
}

func parseArgs() (*Args, error) {
	p := &ArgParser{Args: os.Args[1:], Pos: 0}
	return p.Parse()
}

func (p *ArgParser) IsShorthand(shorthands ...string) bool {
	for _, s := range shorthands {
		if strings.HasPrefix(p.Args[p.Pos], "-"+s) {
			return true
		}
	}
	return false
}

func (p *ArgParser) IsFlag(flags ...string) bool {
	for _, s := range flags {
		if strings.HasPrefix(p.Args[p.Pos], "--"+s) {
			return true
		}
	}
	return false
}

func (p *ArgParser) ConsumeFlagSeparate() (string, error) { // for flags like -E <value> returns <value> or --exclude <value> returns <value>
	flag := p.Args[p.Pos]
	if !p.Skip() {
		return "", fmt.Errorf("expected value after flag %s, but got end of arguments", flag)
	}
	ret := p.Args[p.Pos]
	if strings.HasPrefix(ret, "-") {
		return "", fmt.Errorf("expected value after flag %s, but got another flag: %s", flag, ret)
	}
	p.Skip()
	return ret, nil
}

func (p *ArgParser) Skip() bool {
	if p.HasNext() {
		p.Pos++
		return true
	}
	return false
}

func (p *ArgParser) HasNext() bool {
	return p.Pos < len(p.Args)
}

func (p *ArgParser) Parse() (*Args, error) {
	args := &Args{}

	for p.HasNext() {
		if p.IsShorthand("E") || p.IsFlag("exclude") {
			value, err := p.ConsumeFlagSeparate()
			if err != nil {
				return nil, err
			}
			args.excludeDirs = append(args.excludeDirs, value)
		} else if p.IsShorthand("o") || p.IsFlag("output") {
			value, err := p.ConsumeFlagSeparate()
			if err != nil {
				return nil, err
			}
			args.output = value
		} else if p.IsShorthand("v") || p.IsFlag("verbose") {
			args.verbose = true
			p.Skip()
		} else if p.IsShorthand("d") || p.IsFlag("debug") {
			args.debug = true
			p.Skip()
		} else if !strings.HasPrefix(p.Args[p.Pos], "-") {
			if args.baseDir != "" {
				return nil, fmt.Errorf("multiple base directories specified")
			}
			args.baseDir = p.Args[p.Pos]
			p.Skip()
		} else {
			return nil, fmt.Errorf("unknown argument: %s", p.Args[p.Pos])
		}
	}

	err := validateArgs(args)
	if err != nil {
		return nil, err
	}

	return args, nil
}

func validateArgs(args *Args) error {
	if args.baseDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		args.baseDir = cwd
	}

	if _, err := os.Stat(args.baseDir); os.IsNotExist(err) {
		return fmt.Errorf("base directory does not exist: %s", args.baseDir)
	}

	abs, err := filepath.Abs(args.baseDir)
	if err != nil {
		return fmt.Errorf("failed to get absolute path of base directory: %v", err)
	}
	args.baseDir = abs

	for i, e := range args.excludeDirs {
		if _, err := os.Stat(e); os.IsNotExist(err) {
			return fmt.Errorf("exclude path does not exist: %s", e)
		}

		abs, err := filepath.Abs(e)
		if err != nil {
			return fmt.Errorf("failed to get absolute path of exclude directory: %v", err)
		}
		args.excludeDirs[i] = abs
	}

	return nil
}
