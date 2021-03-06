// Copyright © 2020 Joseph Wright <joseph@cloudboss.co>
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.

package compiler

import (
	"fmt"
	"go/token"
	"reflect"
	"strconv"

	"github.com/cloudboss/unobin/pkg/util"
	"github.com/dave/dst"
)

type Type int

const (
	UnknownType = iota
	ArrayType
	BoolType
	FunctionType
	NumberType
	ObjectType
	StringType
)

var typeRepr = []string{
	"Unknown",
	"Array",
	"Bool",
	"Function",
	"Number",
	"Object",
	"String",
}

// UAST is the Unobin AST, so named to avoid confusion with the Go AST,
// which is generated during compilation.
type UAST struct {
	Attributes ObjectExpr
	Tasks      []*TaskExpr
}

type TaskExpr struct {
	Description string
	Module      string
	When        *FunctionExpr
	Args        ObjectExpr
	Body        []*TaskExpr
	Rescue      []*TaskExpr
	Always      []*TaskExpr
}

type PairExpr struct {
	Name  string
	Value *ValueExpr
}

type ObjectExpr map[string]*ValueExpr

func (o ObjectExpr) ToGoValue() map[string]interface{} {
	m := make(map[string]interface{})
	for k, v := range o {
		m[k] = v.ToGoValue()
	}
	return m
}

func (o ObjectExpr) ToGoAST() dst.Expr {
	cl := &dst.CompositeLit{
		Type: &dst.MapType{
			Key:   &dst.Ident{Name: stringType},
			Value: &dst.Ident{Name: interfaceType},
		},
		Elts: []dst.Expr{},
	}
	for k, v := range o {
		expr := &dst.KeyValueExpr{
			Decs: dst.KeyValueExprDecorations{
				NodeDecs: dst.NodeDecs{
					Before: dst.NewLine,
					After:  dst.NewLine,
				},
			},
			Key:   &dst.BasicLit{Kind: token.STRING, Value: strconv.Quote(k)},
			Value: v.ToGoAST(),
		}
		cl.Elts = append(cl.Elts, expr)
	}
	return cl
}

type ValueExpr struct {
	Array    ArrayExpr
	Bool     *bool
	Function *FunctionExpr
	Number   *NumberExpr
	Object   ObjectExpr
	String   *string
}

func (v *ValueExpr) Equal(other *ValueExpr) bool {
	return reflect.DeepEqual(v, other)
}

func (v *ValueExpr) Type() Type {
	if v.Array != nil {
		return ArrayType
	}
	if v.Bool != nil {
		return BoolType
	}
	if v.Function != nil {
		return FunctionType
	}
	if v.Number != nil {
		return NumberType
	}
	if v.Object != nil {
		return ObjectType
	}
	if v.String != nil {
		return StringType
	}
	return UnknownType
}

func (v *ValueExpr) ToGoValue() interface{} {
	if v.Array != nil {
		return v.Array.ToGoValue()
	}
	if v.Bool != nil {
		return *v.Bool
	}
	if v.Function != nil {
		return v.Function.ToGoValue()
	}
	if v.Number != nil {
		return v.Number.ToGoValue()
	}
	if v.Object != nil {
		return v.Object.ToGoValue()
	}
	if v.String != nil {
		return *v.String
	}
	return nil
}

func (v *ValueExpr) ToGoAST() dst.Expr {
	if v.Array != nil {
		return v.Array.ToGoAST()
	}
	if v.Bool != nil {
		return &dst.BasicLit{Kind: token.STRING, Value: strconv.FormatBool(*v.Bool)}
	}
	if v.Function != nil {
		return v.Function.ToGoAST()
	}
	if v.Number != nil {
		return v.Number.ToGoAST()
	}
	if v.Object != nil {
		return v.Object.ToGoAST()
	}
	if v.String != nil {
		return &dst.BasicLit{
			Kind:  token.STRING,
			Value: strconv.Quote(*v.String),
		}
	}
	return nil
}

type ArrayExpr []*ValueExpr

func (a ArrayExpr) ToGoValue() []interface{} {
	array := make([]interface{}, len(a))
	for i, item := range a {
		array[i] = item.ToGoValue()
	}
	return array
}

func (a ArrayExpr) ToGoAST() dst.Expr {
	cl := &dst.CompositeLit{
		Type: &dst.ArrayType{Elt: &dst.BasicLit{Kind: token.STRING, Value: interfaceType}},
	}
	elts := make([]dst.Expr, len(a))
	for i, el := range a {
		elts[i] = el.ToGoAST()
	}
	cl.Elts = elts
	return cl
}

type FunctionExpr struct {
	Name string
	Args ArrayExpr
}

func (f *FunctionExpr) ToGoValue() []interface{} {
	// A function expression is converted to an array where the first
	// element is the name and the remaining elements are the arguments.
	array := make([]interface{}, len(f.Args)+1)
	array[0] = f.Name
	for i, arg := range f.Args {
		array[i+1] = arg.ToGoValue()
	}
	return array
}

func (f *FunctionExpr) ToGoAST() dst.Expr {
	name := fmt.Sprintf(functionsPackageTemplate, util.KebabToPascal(f.Name))
	args := make([]dst.Expr, len(f.Args)+1)
	for i := range args {
		if i == 0 {
			args[i] = &dst.Ident{Name: ctxVar}
		} else {
			value := f.Args[i-1]
			if value.Type() == FunctionType {
				args[i] = value.ToGoAST()
			} else {
				qi := fmt.Sprintf(functionsPackageTemplate, typeRepr[value.Type()])
				cl := &dst.CompositeLit{
					Type: &dst.BasicLit{Kind: token.STRING, Value: qi},
					Elts: []dst.Expr{
						value.ToGoAST(),
						&dst.BasicLit{Kind: token.STRING, Value: nilValue},
					},
				}
				args[i] = cl
			}
		}
	}
	return &dst.CallExpr{
		Fun:  &dst.Ident{Name: name},
		Args: args,
	}
}

type NumberExpr struct {
	Int   *int64
	Float *float64
}

func (n *NumberExpr) ToGoValue() interface{} {
	if n.Int != nil {
		return *n.Int
	}
	if n.Float != nil {
		return *n.Float
	}
	return nil
}

func (n *NumberExpr) ToGoAST() dst.Expr {
	if n.Int != nil {
		return &dst.BasicLit{Kind: token.INT, Value: fmt.Sprint(*n.Int)}
	}
	if n.Float != nil {
		return &dst.BasicLit{
			Kind:  token.FLOAT,
			Value: strconv.FormatFloat(*n.Float, 'f', -1, 64),
		}
	}
	return nil
}

func NewUAST() *UAST {
	return &UAST{
		Attributes: ObjectExpr{},
		Tasks:      make([]*TaskExpr, 0, 10),
	}
}

func (g *Grammar) LoadUAST() {
	g.uast = NewUAST()
	// g.AST() returns the raw AST created by the PEG parser.
	// We traverse this to populate the UAST.
	node := g.AST()
	for node != nil {
		switch node.pegRule {
		case ruleentry:
			node = node.up
			continue
		case rulestatement:
			g.ruleStatement(node)
		}
		node = node.next
	}
}

func (g *Grammar) ruleStatement(node *node32) {
	node = node.up
	for node != nil {
		switch node.pegRule {
		case rulepair:
			pair := g.rulePair(node)
			g.uast.Attributes[pair.Name] = pair.Value
		case ruletask:
			g.uast.Tasks = append(g.uast.Tasks, g.ruleTask(node))
		}
		node = node.next
	}
}

func (g *Grammar) rulePair(node *node32) *PairExpr {
	node = node.up
	pair := &PairExpr{}
	for node != nil {
		switch node.pegRule {
		case ruleident:
			pair.Name = g.ruleIdent(node)
		case rulestring:
			pair.Name = *g.ruleString(node)
		case rulevalue:
			pair.Value = g.ruleValue(node)
		}
		node = node.next
	}
	return pair
}

func (g *Grammar) ruleTasks(node *node32) []*TaskExpr {
	node = node.up
	tasks := []*TaskExpr{}
	for node != nil {
		switch node.pegRule {
		case ruletask:
			tasks = append(tasks, g.ruleTask(node))
		}
		node = node.next
	}
	return tasks
}

func (g *Grammar) ruleTask(node *node32) *TaskExpr {
	node = node.up
	task := &TaskExpr{}
	for node != nil {
		switch node.pegRule {
		case rulesimple_task:
			task = g.ruleSimpleTask(node)
		case rulecompound_task:
			g.ruleCompoundTask(node, task)
		}
		node = node.next
	}
	return task
}

func (g *Grammar) ruleSimpleTask(node *node32) *TaskExpr {
	node = node.up
	task := &TaskExpr{}
	for node != nil {
		switch node.pegRule {
		case ruletask_description:
			task.Description = g.ruleTaskDescription(node)
		case ruletask_pair:
			g.ruleTaskPair(node, task)
		case rulerescue_clause:
			task.Rescue = g.ruleTasks(node)
		case rulealways_clause:
			task.Always = g.ruleTasks(node)
		}
		node = node.next
	}
	return task
}

func (g *Grammar) ruleModulePair(node *node32) string {
	node = node.up
	module := ""
	for node != nil {
		switch node.pegRule {
		case ruleident:
			module = g.ruleIdent(node)
		}
		node = node.next
	}
	return module
}

func (g *Grammar) ruleArgsPair(node *node32) ObjectExpr {
	node = node.up
	args := ObjectExpr{}
	for node != nil {
		switch node.pegRule {
		case ruleobject:
			args = g.ruleObject(node)
		}
		node = node.next
	}
	return args
}

func (g *Grammar) ruleWhenPair(node *node32) *FunctionExpr {
	node = node.up
	fun := &FunctionExpr{}
	for node != nil {
		switch node.pegRule {
		case rulefun_expr:
			fun = g.ruleFunExpr(node)
		}
		node = node.next
	}
	return fun
}

func (g *Grammar) ruleCompoundTask(node *node32, task *TaskExpr) {
	node = node.up
	for node != nil {
		switch node.pegRule {
		case rulewhen_pair:
			task.When = g.ruleWhenPair(node)
		case ruletask:
			task.Body = append(task.Body, g.ruleTask(node))
		case rulerescue_clause:
			task.Rescue = g.ruleTasks(node)
		case rulealways_clause:
			task.Always = g.ruleTasks(node)
		}
		node = node.next
	}
}

func (g *Grammar) ruleTaskDescription(node *node32) string {
	node = node.up
	str := ""
	for node != nil {
		switch node.pegRule {
		case rulesentence:
			str = g.Buffer[node.begin:node.end]
		}
		node = node.next
	}
	return str
}

func (g *Grammar) ruleTaskPair(node *node32, task *TaskExpr) {
	node = node.up
	for node != nil {
		switch node.pegRule {
		case rulewhen_pair:
			task.When = g.ruleWhenPair(node)
		case rulemodule_pair:
			task.Module = g.ruleModulePair(node)
		case ruleargs_pair:
			task.Args = g.ruleArgsPair(node)
		}
		node = node.next
	}
}

func (g *Grammar) ruleValue(node *node32) *ValueExpr {
	node = node.up
	value := &ValueExpr{}
	for node != nil {
		switch node.pegRule {
		case rulearray:
			array := g.ruleArray(node)
			value.Array = array
		case rulebool_expr:
			boole := g.ruleBoolExpr(node)
			value.Bool = boole
		case rulefun_expr:
			fun := g.ruleFunExpr(node)
			value.Function = fun
		case ruleindex_expr:
			fun := g.ruleIndexExpr(node)
			value.Function = fun
		case rulemath_expr:
			mathExpr := g.ruleMathExpr(node)
			value.Number = mathExpr
		case ruleobject:
			object := g.ruleObject(node)
			value.Object = object
		case rulestring:
			str := g.ruleString(node)
			value.String = str
		}
		node = node.next
	}
	return value
}

func (g *Grammar) ruleArray(node *node32) ArrayExpr {
	node = node.up
	array := ArrayExpr{}
	for node != nil {
		switch node.pegRule {
		case rulevalue:
			value := g.ruleValue(node)
			array = append(array, value)
		}
		node = node.next
	}
	return array
}

func (g *Grammar) ruleBoolExpr(node *node32) *bool {
	node = node.up
	boole := false
	for node != nil {
		switch node.pegRule {
		case ruleTRUE:
			boole = true
		}
		node = node.next
	}
	return &boole
}

func (g *Grammar) ruleFunExpr(node *node32) *FunctionExpr {
	node = node.up
	fun := &FunctionExpr{Args: ArrayExpr{}}
	for node != nil {
		switch node.pegRule {
		case ruleident:
			fun.Name = g.ruleIdent(node)
		case rulevalue:
			fun.Args = append(fun.Args, g.ruleValue(node))
		}
		node = node.next
	}
	return fun
}

func (g *Grammar) ruleIndexExpr(node *node32) *FunctionExpr {
	node = node.up
	fun := &FunctionExpr{Args: ArrayExpr{}}
	for node != nil {
		switch node.pegRule {
		case ruleident_chars:
			fun.Name = g.Buffer[node.begin:node.end]
		case ruleindex_expr_tail:
			fun.Args = append(fun.Args, g.ruleIndexExprTail(node))
		}
		node = node.next
	}
	return fun
}

func (g *Grammar) ruleIndexExprTail(node *node32) *ValueExpr {
	node = node.up
	value := &ValueExpr{}
	for node != nil {
		switch node.pegRule {
		case ruleident_chars:
			value.String = util.StringP(g.Buffer[node.begin:node.end])
		case rulesentence:
			value.String = util.StringP(g.Buffer[node.begin:node.end])
		}
		node = node.next
	}
	return value
}

func (g *Grammar) ruleMathExpr(node *node32) *NumberExpr {
	node = node.up
	number := &NumberExpr{}
	for node != nil {
		switch node.pegRule {
		case rulenumber:
			node = node.up
			continue
		case rulefloat:
			n, _ := strconv.ParseFloat(g.Buffer[node.begin:node.end], 64)
			number.Float = &n
		case ruleint:
			n, _ := strconv.ParseInt(g.Buffer[node.begin:node.end], 10, 64)
			number.Int = &n
		}
		node = node.next
	}
	return number
}

func (g *Grammar) ruleObject(node *node32) ObjectExpr {
	node = node.up
	object := ObjectExpr{}
	for node != nil {
		switch node.pegRule {
		case rulepair:
			pair := g.rulePair(node)
			object[pair.Name] = pair.Value
		}
		node = node.next
	}
	return object
}

func (g *Grammar) ruleIdent(node *node32) string {
	node = node.up
	ident := ""
	for node != nil {
		switch node.pegRule {
		case ruleident_chars:
			ident = g.Buffer[node.begin:node.end]
		}
		node = node.next
	}
	return ident
}

func (g *Grammar) ruleString(node *node32) *string {
	node = node.up
	str := ""
	for node != nil {
		switch node.pegRule {
		case ruleSTRING_DELIM:
			node = node.next
			continue
		case rulestring_body:
			str = g.Buffer[node.begin:node.end]
		}
		node = node.next
	}
	return &str
}
