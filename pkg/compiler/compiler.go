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
	"go/ast"
	"go/parser"
	"go/token"
	"io/ioutil"
	"strconv"

	"github.com/cloudboss/unobin/pkg/module"
	"github.com/cloudboss/unobin/pkg/playbook"
	"github.com/cloudboss/unobin/pkg/util"
	"gopkg.in/yaml.v2"
)

const (
	descriptionField            = "Description"
	ctxField                    = "Context"
	ctxVar                      = "ctx"
	ctxQualifiedIdentifier      = "types.Context"
	errorType                   = "error"
	errVar                      = "err"
	inputSchemaField            = "InputSchema"
	interfaceType               = "interface{}"
	invalidIdentifier           = "InvalidIdentifier"
	lazyPackageTemplate         = "lazy.%s"
	lazySFunction               = "lazy.S"
	maine                       = "main"
	moduleQualifiedIdentifier   = "module.Module"
	modVar                      = "mod"
	nameField                   = "Name"
	nameKey                     = "name"
	nilValue                    = "nil"
	pbVar                       = "pb"
	playbookQualifiedIdentifier = "playbook.Playbook"
	startCLIMethod              = "pb.StartCLI"
	stateField                  = "State"
	stringType                  = "string"
	taskQualifiedIdentifier     = "task.Task"
	tasksField                  = "Tasks"
	unwrapField                 = "Unwrap"
	varsField                   = "Vars"
	whenField                   = "When"
	whenKey                     = "when"
)

type Compiler struct {
	ModuleImports map[string]*module.ModuleImport
	PlaybookRepr  playbook.PlaybookRepr
}

// Load takes the path to a YAML playbook and loads it into the Compiler or returns an error.
func (c *Compiler) Load(path string) error {
	b, err := ioutil.ReadFile(path)
	if err != nil {
		return err
	}

	var pb playbook.PlaybookRepr
	err = yaml.Unmarshal(b, &pb)
	if err != nil {
		return err
	}

	moduleImports, err := validateTasks(pb.Tasks, pb.Imports)
	if err != nil {
		return err
	}

	c.PlaybookRepr = pb
	c.ModuleImports = moduleImports

	return nil
}

// Compile returns an `*ast.File` which can be formatted into Go using `go/format` or `go/printer`.
// The Load() method must be called before this.
func (c *Compiler) Compile() *ast.File {
	file := &ast.File{
		Name: &ast.Ident{
			Name: maine,
		},
		Decls: []ast.Decl{
			c.genDecl_import(),
			c.funcDecl_main(),
		},
		Package: 1,
	}
	return file
}

// genDecl_import creates an `*ast.GenDecl` for all of the playbook's imports.
func (c *Compiler) genDecl_import() *ast.GenDecl {
	specs := []ast.Spec{
		importSpec("github.com/cloudboss/unobin/pkg/lazy"),
		importSpec("github.com/cloudboss/unobin/pkg/module"),
		importSpec("github.com/cloudboss/unobin/pkg/playbook"),
		importSpec("github.com/cloudboss/unobin/pkg/task"),
		importSpec("github.com/cloudboss/unobin/pkg/types"),
	}
	for _, value := range c.ModuleImports {
		specs = append(specs, importSpec(value.GoImportPath))
	}
	return &ast.GenDecl{
		Tok:   token.IMPORT,
		Specs: specs,
	}
}

// funcDecl_main creates an `*ast.FuncDecl` for the playbook's `main` function.
func (c *Compiler) funcDecl_main() *ast.FuncDecl {
	return &ast.FuncDecl{
		Name: &ast.Ident{Name: maine},
		Type: &ast.FuncType{},
		Body: &ast.BlockStmt{
			List: []ast.Stmt{
				c.assignStmt_ctx(),
				c.assignStmt_pb(),
				&ast.ExprStmt{
					X: &ast.CallExpr{
						Fun: &ast.Ident{Name: startCLIMethod},
					},
				},
			},
		},
	}
}

// assignStmt_ctx creates an `*ast.AssignStmt` for the main function's `types.Context`.
func (c *Compiler) assignStmt_ctx() *ast.AssignStmt {
	return &ast.AssignStmt{
		Tok: token.DEFINE,
		Lhs: []ast.Expr{
			&ast.Ident{Name: ctxVar},
		},
		Rhs: []ast.Expr{
			&ast.UnaryExpr{
				Op: token.AND,
				X: &ast.CompositeLit{
					Type: &ast.Ident{Name: ctxQualifiedIdentifier},
					Elts: []ast.Expr{
						&ast.KeyValueExpr{
							Key: &ast.Ident{Name: varsField},
							Value: &ast.CompositeLit{
								Type: &ast.MapType{
									Key:   &ast.Ident{Name: stringType},
									Value: &ast.Ident{Name: interfaceType},
								},
							},
						},
						&ast.KeyValueExpr{
							Key: &ast.Ident{Name: stateField},
							Value: &ast.CompositeLit{
								Type: &ast.MapType{
									Key:   &ast.Ident{Name: stringType},
									Value: &ast.Ident{Name: interfaceType},
								},
							},
						},
					},
				},
			},
		},
	}
}

// assignStmt_pb creates an `*ast.AssignStmt` for the main function's `playbook.Playbook`.
func (c *Compiler) assignStmt_pb() *ast.AssignStmt {
	return &ast.AssignStmt{
		Tok: token.DEFINE,
		Lhs: []ast.Expr{
			&ast.Ident{Name: pbVar},
		},
		Rhs: []ast.Expr{
			&ast.CompositeLit{
				Type: &ast.Ident{Name: playbookQualifiedIdentifier},
				Elts: []ast.Expr{
					&ast.KeyValueExpr{
						Key:   &ast.Ident{Name: nameField},
						Value: &ast.Ident{Name: strconv.Quote(c.PlaybookRepr.Name)},
					},
					&ast.KeyValueExpr{
						Key:   &ast.Ident{Name: descriptionField},
						Value: &ast.Ident{Name: strconv.Quote(c.PlaybookRepr.Description)},
					},
					&ast.KeyValueExpr{
						Key:   &ast.Ident{Name: ctxField},
						Value: &ast.Ident{Name: ctxVar},
					},
					&ast.KeyValueExpr{
						Key:   &ast.Ident{Name: inputSchemaField},
						Value: c.compileInputSchema(c.PlaybookRepr.InputSchema),
					},
					&ast.KeyValueExpr{
						Key:   &ast.Ident{Name: tasksField},
						Value: c.compositeLit_tasks(),
					},
				},
			},
		},
	}
}

// compositeLit_tasks creates an `*ast.CompositeLit` for the playbook's `*task.Task` array.
func (c *Compiler) compositeLit_tasks() *ast.CompositeLit {
	taskExprs := []ast.Expr{}
	for _, task := range c.PlaybookRepr.Tasks {
		taskExpr := &ast.CompositeLit{Elts: []ast.Expr{}}

		// All tasks have been validated, so it is known that the map contains the `name` key.
		name := task[nameKey].(string)

		taskExpr.Elts = append(taskExpr.Elts, &ast.KeyValueExpr{
			Key:   &ast.Ident{Name: nameField},
			Value: &ast.BasicLit{Value: strconv.Quote(name)},
		})
		taskExpr.Elts = append(taskExpr.Elts, &ast.KeyValueExpr{
			Key:   &ast.Ident{Name: unwrapField},
			Value: c.funcLit_unwrap(task),
		})
		if when, ok := task[whenKey].(string); ok {
			taskExpr.Elts = append(taskExpr.Elts, &ast.KeyValueExpr{
				Key:   &ast.Ident{Name: whenField},
				Value: c.compileTaskField(when),
			})
		}
		taskExprs = append(taskExprs, taskExpr)
	}
	return &ast.CompositeLit{
		Type: &ast.ArrayType{Elt: &ast.StarExpr{X: &ast.Ident{Name: taskQualifiedIdentifier}}},
		Elts: taskExprs,
	}
}

// funcLit_unwrap creates an `*ast.FuncLit` for a playbook task's `Unwrap` field.
func (c *Compiler) funcLit_unwrap(task map[string]interface{}) *ast.FuncLit {
	funcLit := ast.FuncLit{
		Type: &ast.FuncType{
			Results: &ast.FieldList{
				List: []*ast.Field{
					{Type: &ast.Ident{Name: moduleQualifiedIdentifier}},
					{Type: &ast.Ident{Name: errorType}},
				},
			},
		},
		Body: &ast.BlockStmt{},
	}

	var alias string
	var moduleImport *module.ModuleImport
	var moduleBody map[interface{}]interface{}

	for alias, moduleImport = range c.ModuleImports {
		var ok bool
		if moduleBody, ok = task[alias].(map[interface{}]interface{}); ok {
			break
		}
	}

	funcLit.Body.List = []ast.Stmt{
		&ast.AssignStmt{
			Tok: token.DEFINE,
			Lhs: []ast.Expr{
				&ast.Ident{Name: modVar},
			},
			Rhs: []ast.Expr{
				&ast.UnaryExpr{
					Op: token.AND,
					X:  &ast.CompositeLit{Type: &ast.Ident{Name: moduleImport.QualifiedIdentifier}},
				},
			},
		},
	}

	for k, v := range moduleBody {
		variable := util.SnakeToCamel(k.(string))
		assignStmt := &ast.AssignStmt{
			Tok: token.DEFINE,
			Lhs: []ast.Expr{
				&ast.Ident{Name: variable},
				&ast.Ident{Name: errVar},
			},
			Rhs: []ast.Expr{
				&ast.CallExpr{
					Fun: c.compileTaskField(v.(string)),
				},
			},
		}
		ifErrStmt := &ast.IfStmt{
			Cond: &ast.BinaryExpr{
				Op: token.NEQ,
				X:  &ast.Ident{Name: errVar},
				Y:  &ast.Ident{Name: nilValue},
			},
			Body: &ast.BlockStmt{
				List: []ast.Stmt{
					&ast.ReturnStmt{
						Results: []ast.Expr{
							&ast.Ident{Name: modVar},
							&ast.Ident{Name: errVar},
						},
					},
				},
			},
		}
		field := util.SnakeToPascal(k.(string))
		assignFieldStmt := &ast.AssignStmt{
			Tok: token.ASSIGN,
			Lhs: []ast.Expr{
				&ast.Ident{Name: fmt.Sprintf("mod.%s", field)},
			},
			Rhs: []ast.Expr{
				&ast.Ident{Name: variable},
			},
		}
		funcLit.Body.List = append(funcLit.Body.List, assignStmt)
		funcLit.Body.List = append(funcLit.Body.List, ifErrStmt)
		funcLit.Body.List = append(funcLit.Body.List, assignFieldStmt)
	}

	funcLit.Body.List = append(funcLit.Body.List, &ast.ReturnStmt{
		Results: []ast.Expr{
			&ast.Ident{Name: modVar},
			&ast.Ident{Name: nilValue},
		},
	})
	return &funcLit
}

// callExpr_lazyS returns an `*ast.CallExpr` which represents a "string literal",
// which is actually a call to `lazy.S` with the string as an argument.
func (c *Compiler) callExpr_lazyS(value string) *ast.CallExpr {
	return &ast.CallExpr{
		Fun: &ast.CallExpr{
			Fun:  &ast.Ident{Name: lazySFunction},
			Args: []ast.Expr{&ast.BasicLit{Value: value}},
		},
		Args: []ast.Expr{&ast.Ident{Name: ctxVar}},
	}
}

// compileInputSchema converts the `InputSchema` of the playbook into an `ast.Expr`.
// This will be an `*ast.CompositeLit` with other nested expressions.
func (c *Compiler) compileInputSchema(input interface{}) ast.Expr {
	switch input.(type) {
	case bool:
		return &ast.BasicLit{Value: strconv.FormatBool(input.(bool))}
	case string:
		return &ast.BasicLit{Value: strconv.Quote(input.(string))}
	case int:
		return &ast.BasicLit{Kind: token.INT, Value: fmt.Sprint(input.(int))}
	case float64:
		return &ast.BasicLit{
			Kind:  token.FLOAT,
			Value: strconv.FormatFloat(input.(float64), 'f', -1, 64),
		}
	case map[string]interface{}:
		cl := &ast.CompositeLit{
			Type: &ast.MapType{
				Key:   &ast.Ident{Name: stringType},
				Value: &ast.Ident{Name: interfaceType},
			},
		}
		for k, v := range input.(map[string]interface{}) {
			cl.Elts = append(cl.Elts, &ast.KeyValueExpr{
				Key: c.compileInputSchema(k), Value: c.compileInputSchema(v),
			})
		}
		return cl
	case map[interface{}]interface{}:
		cl := &ast.CompositeLit{
			Type: &ast.MapType{
				Key:   &ast.Ident{Name: stringType},
				Value: &ast.Ident{Name: interfaceType},
			},
		}
		for k, v := range input.(map[interface{}]interface{}) {
			cl.Elts = append(cl.Elts, &ast.KeyValueExpr{
				Key: c.compileInputSchema(k), Value: c.compileInputSchema(v),
			})
		}
		return cl
	case []interface{}:
		cl := &ast.CompositeLit{
			Type: &ast.ArrayType{Elt: &ast.BasicLit{Value: interfaceType}},
		}
		in := input.([]interface{})
		elts := make([]ast.Expr, len(in))
		for i, el := range in {
			elts[i] = c.compileInputSchema(el)
		}
		cl.Elts = elts
		return cl
	default:
		// TODO: Validate earlier if possible, but this will at least
		// cause a compile error later when compiling the Go.
		return &ast.Ident{Name: invalidIdentifier}
	}
}

// compileTaskField takes the value of a playbook task's field and parses it to obtain
// a function call expression. Given that it uses the Go parser, a playbook function call
// must be valid Go syntax. If the expression does not parse as a Go `*ast.CallExpr`, it
// is treated as a literal string.
func (c *Compiler) compileTaskField(field string) ast.Expr {
	expr, err := parser.ParseExprFrom(token.NewFileSet(), "", field, parser.AllErrors)
	if err != nil {
		// A parse error will be treated as a literal string.
		return c.callExpr_lazyS(strconv.Quote(field))
	}
	switch expr.(type) {
	case *ast.CallExpr:
		return c.compileModuleExpr(expr)
	default:
		// Anything parsed as other than function call will be treated as a literal string.
		return c.callExpr_lazyS(strconv.Quote(field))
	}
}

// compileModuleExpr is initially given an `*ast.CallExpr` parsed from the playbook and recursively
// produces an `*ast.CallExpr` from it. When it reaches an `*ast.BasicLit`, it returns a literal string.
func (c *Compiler) compileModuleExpr(expr ast.Expr) ast.Expr {
	switch expr.(type) {
	case *ast.CallExpr:
		ce := expr.(*ast.CallExpr)
		f := c.funcName(ce.Fun)
		args := make([]ast.Expr, len(ce.Args))
		for i, arg := range ce.Args {
			args[i] = c.compileModuleExpr(arg)
		}
		return &ast.CallExpr{
			Fun: &ast.CallExpr{
				Fun:  &ast.Ident{Name: f},
				Args: args,
			},
			Args: []ast.Expr{
				&ast.Ident{Name: ctxVar},
			},
		}
	case *ast.BasicLit:
		value := expr.(*ast.BasicLit).Value
		return c.callExpr_lazyS(value)
	default:
		// TODO: Better validation.
		return &ast.CallExpr{Fun: &ast.Ident{Name: invalidIdentifier}}
	}
}

// funcName gets the function name from an `*ast.CallExpr` which has been parsed from
// the playbook and returns a Go qualified identifier for it.
func (c *Compiler) funcName(expr ast.Expr) string {
	switch expr.(type) {
	case *ast.Ident:
		value := expr.(*ast.Ident).Name
		return fmt.Sprintf(lazyPackageTemplate, util.SnakeToPascal(value))
	default:
		// TODO: Better validation.
		return invalidIdentifier
	}
}

// validateTasks validates all the given tasks using `validateTask`. On success, a map of
// `*module.ModuleImport` is returned with the keys being the name given to the import in the playbook,
// otherwise error.
func validateTasks(tasks []map[string]interface{}, imports map[string]string) (map[string]*module.ModuleImport, error) {
	modules := map[string]*module.ModuleImport{}
	for _, task := range tasks {
		moduleImport, err := validateTask(task, imports)
		if err != nil {
			return nil, err
		}
		modules[moduleImport.Alias] = moduleImport
	}
	return modules, nil
}

// validateTask ensures that a playbook task has the correct attributes set, and that the task's module is
// defined in the playbook's imports. On success, a `*module.ModuleImport` is returned, otherwise error.
func validateTask(task map[string]interface{}, imports map[string]string) (*module.ModuleImport, error) {
	// Make a copy of the task to modify.
	taskCopy := map[string]interface{}{}
	for k, v := range task {
		taskCopy[k] = v
	}

	requiredAttrs := []string{
		nameKey,
	}
	optionalAttrs := []string{
		whenKey,
	}
	for _, attr := range requiredAttrs {
		if _, ok := taskCopy[attr]; !ok {
			return nil, fmt.Errorf("missing attribute '%s' from task %+v", attr, task)
		}
		delete(taskCopy, attr)
	}
	for _, attr := range optionalAttrs {
		delete(taskCopy, attr)
	}

	// The only remaining attribute in the task should be the alias defining the module.
	if len(taskCopy) != 1 {
		return nil, fmt.Errorf("unknown attributes defined on task %+v", task)
	}

	for alias, body := range taskCopy {
		// Basic type check to ensure the type of a module body is a map.
		if _, ok := body.(map[interface{}]interface{}); !ok {
			return nil, fmt.Errorf("type of module body should be a map for task %+v", task)
		}
		// The alias defining the module must match a key in `imports`.
		if importPath, ok := imports[alias]; !ok {
			return nil, fmt.Errorf("unknown module for task %+v", task)
		} else {
			return module.NewModuleImport(alias, importPath)
		}
	}
	// Should not reach here.
	return nil, nil
}

// importSpec creates an `*ast.ImportSpec` for a single import.
func importSpec(path string) *ast.ImportSpec {
	return &ast.ImportSpec{
		Path: &ast.BasicLit{Value: strconv.Quote(path)},
	}
}
