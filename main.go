/* Copyright (c) 2015, Daniel Martí <mvdan@mvdan.cc> */
/* See LICENSE for licensing information */

package main

import (
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"log"
	"os"
)

func typeMap(t *types.Tuple) map[string]types.Type {
	m := make(map[string]types.Type, t.Len())
	for i := 0; i < t.Len(); i++ {
		p := t.At(i)
		m[p.Name()] = p.Type()
	}
	return m
}

func init() {
	typesInit()
}

type call struct {
	params  []interface{}
	results []interface{}
}

var toToken = map[string]token.Token{
	"byte":    token.INT,
	"int":     token.INT,
	"int8":    token.INT,
	"int16":   token.INT,
	"int32":   token.INT,
	"int64":   token.INT,
	"uint":    token.INT,
	"uint8":   token.INT,
	"uint16":  token.INT,
	"uint32":  token.INT,
	"uint64":  token.INT,
	"string":  token.STRING,
	"float32": token.FLOAT,
	"float64": token.FLOAT,
}

func paramEqual(t1 types.Type, a2 interface{}) bool {
	switch x := a2.(type) {
	case string:
		return t1.String() == x
	case token.Token:
		return toToken[t1.String()] == x
	case nil:
		switch t1.(type) {
		case *types.Slice:
			return true
		case *types.Map:
			return true
		default:
			return false
		}
	default:
		return false
	}
}

func paramsMatch(types1 []types.Type, args2 []interface{}) bool {
	if len(types1) != len(args2) {
		return false
	}
	for i, t1 := range types1 {
		a2 := args2[i]
		if !paramEqual(t1, a2) {
			return false
		}
	}
	return true
}

func resultEqual(t1 types.Type, e2 interface{}) bool {
	switch x := e2.(type) {
	case string:
		return t1.String() == x
	case nil:
		// assigning to _
		return true
	default:
		return false
	}
}

func resultsMatch(types1 []types.Type, exps2 []interface{}) bool {
	if len(exps2) == 0 {
		return true
	}
	if len(types1) != len(exps2) {
		return false
	}
	for i, t1 := range types1 {
		e2 := exps2[i]
		if !resultEqual(t1, e2) {
			return false
		}
	}
	return true
}

func interfaceMatching(calls map[string]call) string {
	matchesIface := func(decls map[string]funcDecl) bool {
		if len(calls) > len(decls) {
			return false
		}
		for n, d := range decls {
			c, e := calls[n]
			if !e {
				return false
			}
			if !paramsMatch(d.params, c.params) {
				return false
			}
			if !resultsMatch(d.results, c.results) {
				return false
			}
		}
		return true
	}
	for name, decls := range parsed {
		if matchesIface(decls) {
			return name
		}
	}
	return ""
}

func main() {
	parseFile("stdin.go", os.Stdin, os.Stdout)
}

func parseFile(name string, r io.Reader, w io.Writer) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, name, r, 0)
	if err != nil {
		log.Fatal(err)
	}

	conf := types.Config{Importer: importer.Default()}
	pkg, err := conf.Check("", fset, []*ast.File{f}, nil)
	if err != nil {
		log.Fatal(err)
	}

	v := &Visitor{
		w:      w,
		fset:   fset,
		scopes: []*types.Scope{pkg.Scope()},
	}
	ast.Walk(v, f)
}

type Visitor struct {
	w      io.Writer
	fset   *token.FileSet
	scopes []*types.Scope

	nodes []ast.Node

	params map[string]types.Type
	used   map[string]map[string]call
}

func scopeName(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.StarExpr:
		return scopeName(x.X)
	default:
		return ""
	}
}

func (v *Visitor) getFuncType(fd *ast.FuncDecl) *types.Func {
	name := fd.Name.Name
	scope := v.scopes[len(v.scopes)-1]
	if fd.Recv == nil {
		return scope.Lookup(name).(*types.Func)
	}
	if len(fd.Recv.List) > 1 {
		return nil
	}
	tname := scopeName(fd.Recv.List[0].Type)
	st := scope.Lookup(tname).(*types.TypeName)
	named := st.Type().(*types.Named)
	for i := 0; i < named.NumMethods(); i++ {
		f := named.Method(i)
		if f.Name() == name {
			return f
		}
	}
	return nil
}

type assignCall struct {
	left  []ast.Expr
	right *ast.CallExpr
}

func assignCalls(as *ast.AssignStmt) []assignCall {
	if len(as.Rhs) == 1 {
		ce, ok := as.Rhs[0].(*ast.CallExpr)
		if !ok {
			return nil
		}
		return []assignCall{
			{
				left:  as.Lhs,
				right: ce,
			},
		}
	}
	var calls []assignCall
	for i, right := range as.Rhs {
		ce, ok := right.(*ast.CallExpr)
		if !ok {
			continue
		}
		calls = append(calls, assignCall{
			left:  []ast.Expr{as.Lhs[i]},
			right: ce,
		})
	}
	return calls
}

func (v *Visitor) Visit(node ast.Node) ast.Visitor {
	switch x := node.(type) {
	case *ast.File:
	case *ast.FuncDecl:
		f := v.getFuncType(x)
		if f == nil {
			return nil
		}
		v.scopes = append(v.scopes, f.Scope())
		sign := f.Type().(*types.Signature)
		v.params = typeMap(sign.Params())
		v.used = make(map[string]map[string]call, 0)
	case *ast.BlockStmt:
	case *ast.ExprStmt:
	case *ast.AssignStmt:
		calls := assignCalls(x)
		for _, c := range calls {
			v.onCall(c.left, c.right)
		}
		return nil
	case *ast.CallExpr:
		v.onCall(nil, x)
	case nil:
		top := v.nodes[len(v.nodes)-1]
		v.nodes = v.nodes[:len(v.nodes)-1]
		if _, ok := top.(*ast.FuncDecl); !ok {
			return nil
		}
		v.scopes = v.scopes[:len(v.scopes)-1]
		for name, methods := range v.used {
			iface := interfaceMatching(methods)
			if iface == "" {
				continue
			}
			if iface == v.params[name].String() {
				continue
			}
			pos := v.fset.Position(top.Pos())
			fmt.Fprintf(v.w, "%s:%d: %s can be %s\n",
				pos.Filename, pos.Line, name, iface)
		}
		v.params = nil
		v.used = nil
	default:
		return nil
	}
	if node != nil {
		v.nodes = append(v.nodes, node)
	}
	return v
}

func getType(scope *types.Scope, name string) interface{} {
	if scope == nil {
		return nil
	}
	obj := scope.Lookup(name)
	if obj == nil {
		return getType(scope.Parent(), name)
	}
	switch x := obj.(type) {
	case *types.Var:
		return x.Type().String()
	default:
		return nil
	}
}

func (v *Visitor) descType(e ast.Expr) interface{} {
	switch x := e.(type) {
	case *ast.Ident:
		scope := v.scopes[len(v.scopes)-1]
		return getType(scope, x.Name)
	case *ast.BasicLit:
		return x.Kind
	default:
		return nil
	}
}

func (v *Visitor) onCall(results []ast.Expr, ce *ast.CallExpr) {
	sel, ok := ce.Fun.(*ast.SelectorExpr)
	if !ok {
		return
	}
	left, ok := sel.X.(*ast.Ident)
	if !ok {
		return
	}
	right := sel.Sel
	vname := left.Name
	fname := right.Name
	c := call{}
	for _, r := range results {
		c.results = append(c.results, v.descType(r))
	}
	for _, a := range ce.Args {
		c.params = append(c.params, v.descType(a))
	}
	if _, e := v.used[vname]; !e {
		v.used[vname] = make(map[string]call)
	}
	v.used[vname][fname] = c
}
