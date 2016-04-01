// Copyright (c) 2015, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package interfacer

import (
	"go/types"

	"golang.org/x/tools/go/loader"

	"github.com/mvdan/interfacer/internal/util"
)

//go:generate sh -c "go list std | go run generate/std/main.go -o std.go"
//go:generate gofmt -w -s std.go

type cache struct {
	loader.Config

	ifaces map[string]string
	funcs  map[string]string

	grabbed map[string]struct{}
}

func newCache() *cache {
	c := &cache{
		ifaces:  make(map[string]string),
		funcs:   make(map[string]string),
		grabbed: make(map[string]struct{}),
	}
	c.AllowErrors = true
	c.TypeChecker.Error = func(e error) {}
	c.TypeChecker.DisableUnusedImportCheck = true
	c.TypeCheckFuncBodies = func(path string) bool {
		_, e := stdPkgs[path]
		return !e
	}
	return c
}

func (c *cache) funcOf(t string) string {
	if s := stdFuncs[t]; s != "" {
		return s
	}
	return c.funcs[t]
}

func (c *cache) ifaceOf(t string) string {
	if s := stdIfaces[t]; s != "" {
		return s
	}
	return c.ifaces[t]
}

func (c *cache) typesGet(pkgs []*types.Package) {
	for _, pkg := range pkgs {
		path := pkg.Path()
		if _, e := stdPkgs[path]; e {
			continue
		}
		if _, e := c.grabbed[path]; e {
			continue
		}
		c.grabbed[path] = struct{}{}
		c.grabExported(pkg)
		c.typesGet(pkg.Imports())
	}
}

func (c *cache) grabExported(pkg *types.Package) {
	ifs, funs := FromScope(pkg.Scope())
	for iftype, ifname := range ifs {
		if !util.Exported(ifname) {
			continue
		}
		if _, e := stdIfaces[iftype]; e {
			continue
		}
		c.ifaces[iftype] = pkg.Path() + "." + ifname
	}
	for ftype, fname := range funs {
		if !util.Exported(fname) {
			continue
		}
		if _, e := stdFuncs[ftype]; e {
			continue
		}
		c.funcs[ftype] = pkg.Path() + "." + fname
	}
}
