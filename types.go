// Copyright (c) 2015, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package interfacer

import (
	"strings"

	"golang.org/x/tools/go/loader"
	"golang.org/x/tools/go/types"
)

//go:generate go run generate/std/main.go generate/std/pkgs.go -o std.go
//go:generate gofmt -w -s std.go

type cache struct {
	loader.Config
}

func typesInit(paths []string) {
	c = &cache{}
	c.AllowErrors = true
	c.TypeChecker.Error = func(e error) {}
	c.TypeChecker.DisableUnusedImportCheck = true
	argPaths := make(map[string]bool, len(paths))
	for _, path := range paths {
		argPaths[path] = true
	}
	c.TypeCheckFuncBodies = func(path string) bool {
		if _, e := pkgs[path]; e {
			return false
		}
		if !strings.Contains(path, "/") {
			return true
		}
		return argPaths[path]
	}
}

func typesGet(prog *loader.Program) {
	for _, pkg := range prog.InitialPackages() {
		grabExported(pkg.Pkg.Scope(), pkg.Pkg.Path())
	}
}

func grabExported(scope *types.Scope, path string) {
	ifs, funs := FromScope(scope)
	for iftype, ifname := range ifs {
		if _, e := ifaces[iftype]; e {
			continue
		}
		ifaces[iftype] = path + "." + ifname
	}
	for ftype, fname := range funs {
		if _, e := funcs[ftype]; e {
			continue
		}
		funcs[ftype] = path + "." + fname
	}
}
