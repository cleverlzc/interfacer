// Copyright (c) 2015, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package interfacer

import (
	"fmt"
	"go/ast"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/tools/go/loader"
	"golang.org/x/tools/go/types"
)

// TODO: don't use a global state to allow concurrent use
var c *cache

func implementsIface(sign *types.Signature) bool {
	s := signString(sign)
	_, e := funcs[s]
	return e
}

func doMethoderType(t types.Type) map[string]string {
	switch x := t.(type) {
	case *types.Pointer:
		return doMethoderType(x.Elem())
	case *types.Named:
		if u, ok := x.Underlying().(*types.Interface); ok {
			return doMethoderType(u)
		}
		return namedMethodMap(x)
	case *types.Interface:
		return ifaceFuncMap(x)
	default:
		return nil
	}
}

func assignable(s, t string, called, want map[string]string) bool {
	if s == t {
		return true
	}
	if len(t) >= len(s) {
		return false
	}
	for fname, ftype := range want {
		s, e := called[fname]
		if !e || s != ftype {
			return false
		}
	}
	return true
}

func interfaceMatching(p *param) (string, string) {
	for to := range p.assigned {
		if to.discard {
			return "", ""
		}
	}
	allFuncs := doMethoderType(p.t)
	called := make(map[string]string, len(p.calls))
	for fname := range p.calls {
		called[fname] = allFuncs[fname]
	}
	s := funcMapString(called)
	name, e := ifaces[s]
	if !e {
		return "", ""
	}
	for t := range p.usedAs {
		iface, ok := t.(*types.Interface)
		if !ok {
			return "", ""
		}
		asMethods := ifaceFuncMap(iface)
		as := funcMapString(asMethods)
		if !assignable(s, as, called, asMethods) {
			return "", ""
		}
	}
	return name, s
}

func orderedPkgs(prog *loader.Program) ([]*types.Package, error) {
	// TODO: InitialPackages() is not in the order that we passed to
	// it via Import() calls.
	// For now, make it deterministic by sorting by import path.
	unordered := prog.InitialPackages()
	paths := make([]string, 0, len(unordered))
	byPath := make(map[string]*types.Package, len(unordered))
	for _, info := range unordered {
		if info.Errors != nil {
			return nil, info.Errors[0]
		}
		path := info.Pkg.Path()
		paths = append(paths, path)
		byPath[path] = info.Pkg
	}
	sort.Sort(ByAlph(paths))
	pkgs := make([]*types.Package, 0, len(unordered))
	for _, path := range paths {
		pkgs = append(pkgs, byPath[path])
	}
	return pkgs, nil
}

// relPathErr makes converts errors by go/types and go/loader that use
// absolute paths into errors with relative paths
func relPathErr(err error) error {
	errStr := fmt.Sprintf("%v", err)
	if !strings.HasPrefix(errStr, "/") {
		return err
	}
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	if strings.HasPrefix(errStr, wd) {
		return fmt.Errorf(errStr[len(wd)+1:])
	}
	return err
}

func CheckArgs(args []string, w io.Writer, verbose bool) error {
	paths, err := recurse(args)
	if err != nil {
		return err
	}
	typesInit()
	if _, err := c.FromArgs(paths, false); err != nil {
		return err
	}
	prog, err := c.Load()
	if err != nil {
		return err
	}
	pkgs, err := orderedPkgs(prog)
	if err != nil {
		return relPathErr(err)
	}
	typesGet(pkgs)
	for _, pkg := range pkgs {
		info := prog.AllPackages[pkg]
		if verbose {
			fmt.Fprintln(w, info.Pkg.Path())
		}
		checkPkg(&c.TypeChecker, info, w)
	}
	return nil
}

func checkPkg(conf *types.Config, info *loader.PackageInfo, w io.Writer) {
	v := &visitor{
		PackageInfo: info,
		w:           w,
		fset:        c.Fset,
	}
	for _, f := range info.Files {
		ast.Walk(v, f)
	}
}

type param struct {
	t   types.Type
	pos token.Pos

	calls   map[string]struct{}
	usedAs  map[types.Type]struct{}
	discard bool

	assigned map[*param]struct{}
}

type visitor struct {
	*loader.PackageInfo

	w     io.Writer
	fset  *token.FileSet
	signs []*types.Signature

	params  map[types.Object]*param
	extras  map[types.Object]*param
	inBlock bool

	skipNext bool
}

func paramsMap(t *types.Tuple) map[types.Object]*param {
	m := make(map[types.Object]*param, t.Len())
	for i := 0; i < t.Len(); i++ {
		p := t.At(i)
		m[p] = &param{
			t:        p.Type(),
			pos:      p.Pos(),
			calls:    make(map[string]struct{}),
			usedAs:   make(map[types.Type]struct{}),
			assigned: make(map[*param]struct{}),
		}
	}
	return m
}

func paramType(sign *types.Signature, i int) types.Type {
	params := sign.Params()
	extra := sign.Variadic() && i >= params.Len()-1
	if !extra {
		if i >= params.Len() {
			// builtins with multiple signatures
			return nil
		}
		return params.At(i).Type()
	}
	last := params.At(params.Len() - 1)
	switch x := last.Type().(type) {
	case *types.Slice:
		return x.Elem()
	default:
		return x
	}
}

func (v *visitor) param(id *ast.Ident) *param {
	obj := v.ObjectOf(id)
	if obj == nil {
		panic("unexpected nil object found")
	}
	if p, e := v.params[obj]; e {
		return p
	}
	if p, e := v.extras[obj]; e {
		return p
	}
	p := &param{
		calls:    make(map[string]struct{}),
		usedAs:   make(map[types.Type]struct{}),
		assigned: make(map[*param]struct{}),
	}
	v.extras[obj] = p
	return p
}

func (v *visitor) addUsed(id *ast.Ident, as types.Type) {
	if as == nil {
		return
	}
	p := v.param(id)
	p.usedAs[as.Underlying()] = struct{}{}
}

func (v *visitor) addAssign(to, from *ast.Ident) {
	pto := v.param(to)
	pfrom := v.param(from)
	pfrom.assigned[pto] = struct{}{}
}

func (v *visitor) discard(e ast.Expr) {
	if !v.inBlock {
		return
	}
	id, ok := e.(*ast.Ident)
	if !ok {
		return
	}
	p := v.param(id)
	p.discard = true
}

func (v *visitor) Visit(node ast.Node) ast.Visitor {
	if v.skipNext {
		v.skipNext = false
		return nil
	}
	var sign *types.Signature
	switch x := node.(type) {
	case *ast.FuncLit:
		if v.inBlock {
			break
		}
		sign = v.Types[x].Type.(*types.Signature)
		if implementsIface(sign) {
			return nil
		}
		v.params = paramsMap(sign.Params())
		v.extras = make(map[types.Object]*param)
	case *ast.FuncDecl:
		sign = v.Defs[x.Name].Type().(*types.Signature)
		if implementsIface(sign) {
			return nil
		}
		v.params = paramsMap(sign.Params())
		v.extras = make(map[types.Object]*param)
	case *ast.BlockStmt:
		if v.params != nil {
			v.inBlock = true
		}
	case *ast.SelectorExpr:
		v.discard(x.X)
	case *ast.UnaryExpr:
		v.discard(x.X)
	case *ast.BinaryExpr:
		v.discard(x.X)
		v.discard(x.Y)
	case *ast.IndexExpr:
		v.discard(x.X)
	case *ast.IncDecStmt:
		v.discard(x.X)
	case *ast.AssignStmt:
		if !v.inBlock {
			return nil
		}
		v.onAssign(x)
	case *ast.CallExpr:
		if !v.inBlock {
			return nil
		}
		v.onCall(x)
	case nil:
		top := v.signs[len(v.signs)-1]
		if top != nil {
			v.funcEnded(top)
			v.params = nil
			v.extras = nil
			v.inBlock = false
		}
		v.signs = v.signs[:len(v.signs)-1]
	}
	if node != nil {
		v.signs = append(v.signs, sign)
	}
	return v
}

func funcSignature(t types.Type) *types.Signature {
	switch x := t.(type) {
	case *types.Signature:
		return x
	case *types.Named:
		return funcSignature(x.Underlying())
	default:
		return nil
	}
}

func (v *visitor) onAssign(as *ast.AssignStmt) {
	for i, e := range as.Rhs {
		id, ok := e.(*ast.Ident)
		if !ok {
			continue
		}
		left := as.Lhs[i]
		v.addUsed(id, v.Types[left].Type)
		if lid, ok := left.(*ast.Ident); ok {
			v.addAssign(lid, id)
		}
	}
}

func (v *visitor) onCall(ce *ast.CallExpr) {
	switch y := ce.Fun.(type) {
	case *ast.Ident:
		v.skipNext = true
	case *ast.SelectorExpr:
		if _, ok := y.X.(*ast.Ident); ok {
			v.skipNext = true
		}
	}
	sign := funcSignature(v.Types[ce.Fun].Type)
	if sign == nil {
		return
	}
	for i, e := range ce.Args {
		if id, ok := e.(*ast.Ident); ok {
			v.addUsed(id, paramType(sign, i))
		}
	}
	sel, ok := ce.Fun.(*ast.SelectorExpr)
	if !ok {
		return
	}
	left, ok := sel.X.(*ast.Ident)
	if !ok {
		return
	}
	p := v.param(left)
	p.calls[sel.Sel.Name] = struct{}{}
	return
}

func (v *visitor) funcEnded(sign *types.Signature) {
	for obj, p := range v.params {
		if p.discard {
			continue
		}
		ifname, iftype := interfaceMatching(p)
		if ifname == "" {
			continue
		}
		if _, haveIface := p.t.Underlying().(*types.Interface); haveIface {
			if ifname == p.t.String() {
				continue
			}
			have := funcMapString(doMethoderType(p.t))
			if have == iftype {
				continue
			}
		}
		pos := v.fset.Position(p.pos)
		fname := pos.Filename
		if fname[0] == '/' {
			fname = filepath.Join(v.Pkg.Path(), filepath.Base(fname))
		}
		pname := v.Pkg.Name()
		if strings.HasPrefix(ifname, pname+".") {
			ifname = ifname[len(pname)+1:]
		}
		fmt.Fprintf(v.w, "%s:%d:%d: %s can be %s\n",
			fname, pos.Line, pos.Column, obj.Name(), ifname)
	}
}
