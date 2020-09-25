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
	inputSchemaField            = "InputSchema"
	interfaceType               = "interface{}"
	invalidIdentifier           = "InvalidIdentifier"
	lazyPackageTemplate         = "lazy.%s"
	lazySFunction               = "lazy.S"
	maine                       = "main"
	moduleField                 = "Module"
	nameField                   = "Name"
	nameKey                     = "name"
	pbVar                       = "pb"
	playbookQualifiedIdentifier = "playbook.Playbook"
	startCLIMethod              = "pb.StartCLI"
	stateField                  = "State"
	stringType                  = "string"
	taskQualifiedIdentifier     = "task.Task"
	tasksField                  = "Tasks"
	varsField                   = "Vars"
	whenField                   = "When"
	whenKey                     = "when"
)

// Load takes the path to a YAML playbook and returns a `*playbook.PlaybookRepr` or an error.
func Load(path string) (*playbook.PlaybookRepr, map[string]*module.ModuleImport, error) {
	b, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	var pb playbook.PlaybookRepr
	err = yaml.Unmarshal(b, &pb)
	if err != nil {
		return nil, nil, err
	}
	modules, err := validateTasks(pb.Tasks, pb.Imports)
	return &pb, modules, err
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

// Compile takes a `*playbook.PlaybookRepr` and returns an `*ast.File` which can
// be formatted into Go using `go/format` or `go/printer`.
func Compile(pb *playbook.PlaybookRepr, modules map[string]*module.ModuleImport) *ast.File {
	file := &ast.File{
		Name: &ast.Ident{
			Name: maine,
		},
		Decls: []ast.Decl{
			genDecl_import(modules),
			funcDecl_main(pb, modules),
		},
		Package: 1,
	}
	return file
}

// genDecl_import creates an `*ast.GenDecl` for all of the playbook's imports.
func genDecl_import(imports map[string]*module.ModuleImport) *ast.GenDecl {
	specs := []ast.Spec{
		importSpec("github.com/cloudboss/unobin/pkg/lazy"),
		importSpec("github.com/cloudboss/unobin/pkg/playbook"),
		importSpec("github.com/cloudboss/unobin/pkg/task"),
		importSpec("github.com/cloudboss/unobin/pkg/types"),
	}
	for _, value := range imports {
		specs = append(specs, importSpec(value.GoImportPath))
	}
	return &ast.GenDecl{
		Tok:   token.IMPORT,
		Specs: specs,
	}
}

// importSpec creates an `*ast.ImportSpec` for a single import.
func importSpec(path string) *ast.ImportSpec {
	return &ast.ImportSpec{
		Path: &ast.BasicLit{Value: strconv.Quote(path)},
	}
}

// funcDecl_main creates an `*ast.FuncDecl` for the playbook's `main` function.
func funcDecl_main(pb *playbook.PlaybookRepr, modules map[string]*module.ModuleImport) *ast.FuncDecl {
	return &ast.FuncDecl{
		Name: &ast.Ident{Name: maine},
		Type: &ast.FuncType{},
		Body: &ast.BlockStmt{
			List: []ast.Stmt{
				assignStmt_ctx(),
				assignStmt_pb(pb, modules),
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
func assignStmt_ctx() *ast.AssignStmt {
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
func assignStmt_pb(pb *playbook.PlaybookRepr, modules map[string]*module.ModuleImport) *ast.AssignStmt {
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
						Value: &ast.Ident{Name: strconv.Quote(pb.Name)},
					},
					&ast.KeyValueExpr{
						Key:   &ast.Ident{Name: descriptionField},
						Value: &ast.Ident{Name: strconv.Quote(pb.Description)},
					},
					&ast.KeyValueExpr{
						Key:   &ast.Ident{Name: ctxField},
						Value: &ast.Ident{Name: ctxVar},
					},
					&ast.KeyValueExpr{
						Key:   &ast.Ident{Name: inputSchemaField},
						Value: compileInputSchema(pb.InputSchema),
					},
					&ast.KeyValueExpr{
						Key:   &ast.Ident{Name: tasksField},
						Value: compositeLit_tasks(pb, modules),
					},
				},
			},
		},
	}
}

// compositeLit_tasks creates an `*ast.CompositeLit` for the playbook's `*task.Task` array.
func compositeLit_tasks(pb *playbook.PlaybookRepr, modules map[string]*module.ModuleImport) *ast.CompositeLit {
	taskExprs := []ast.Expr{}
	for _, task := range pb.Tasks {
		taskExpr := &ast.CompositeLit{Elts: []ast.Expr{}}

		// All tasks have been validated, so it is known that the map contains the `name` key.
		name := task[nameKey].(string)

		taskExpr.Elts = append(taskExpr.Elts, &ast.KeyValueExpr{
			Key:   &ast.Ident{Name: nameField},
			Value: &ast.BasicLit{Value: strconv.Quote(name)},
		})
		taskExpr.Elts = append(taskExpr.Elts, &ast.KeyValueExpr{
			Key: &ast.Ident{Name: moduleField},
			Value: &ast.UnaryExpr{
				Op: token.AND,
				X:  compositeLit_module(task, modules),
			},
		})
		if when, ok := task[whenKey].(string); ok {
			taskExpr.Elts = append(taskExpr.Elts, &ast.KeyValueExpr{
				Key:   &ast.Ident{Name: whenField},
				Value: compileModuleField(when),
			})
		}
		taskExprs = append(taskExprs, taskExpr)
	}
	return &ast.CompositeLit{
		Type: &ast.ArrayType{Elt: &ast.StarExpr{X: &ast.Ident{Name: taskQualifiedIdentifier}}},
		Elts: taskExprs,
	}
}

// compositeLit_module creates an `*ast.CompositeLit` for a playbook task's `module.Module`.
func compositeLit_module(task map[string]interface{}, modules map[string]*module.ModuleImport) *ast.CompositeLit {
	cl := ast.CompositeLit{}
	for alias, moduleImport := range modules {
		var body map[interface{}]interface{}
		var ok bool
		if body, ok = task[alias].(map[interface{}]interface{}); !ok {
			continue
		}

		cl.Type = &ast.Ident{Name: moduleImport.QualifiedIdentifier}
		for k, v := range body {
			key := util.SnakeToPascal(k.(string))
			value := compileModuleField(v.(string))
			cl.Elts = append(cl.Elts, &ast.KeyValueExpr{
				Key:   &ast.Ident{Name: key},
				Value: value,
			})
		}
	}
	return &cl
}

// callExpr_lazyS returns an `*ast.CallExpr` which represents a "string literal",
// which is actually a call to `lazy.S` with the string as an argument.
func callExpr_lazyS(value string) *ast.CallExpr {
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
func compileInputSchema(input interface{}) ast.Expr {
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
				Key: compileInputSchema(k), Value: compileInputSchema(v),
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
				Key: compileInputSchema(k), Value: compileInputSchema(v),
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
			elts[i] = compileInputSchema(el)
		}
		cl.Elts = elts
		return cl
	default:
		// TODO: Validate earlier if possible, but this will at least
		// cause a compile error later when compiling the Go.
		return &ast.Ident{Name: invalidIdentifier}
	}
}

// compileModuleField takes the value of a playbook module's field and parses it to obtain
// a function call expression. Given that it uses the Go parser, a playbook function call
// must be valid Go syntax. If the expression does not parse as a Go `*ast.CallExpr`, it
// is treated as a literal string.
func compileModuleField(field string) ast.Expr {
	expr, err := parser.ParseExprFrom(token.NewFileSet(), "", field, parser.AllErrors)
	if err != nil {
		// A parse error will be treated as a literal string.
		return callExpr_lazyS(strconv.Quote(field))
	}
	switch expr.(type) {
	case *ast.CallExpr:
		return compileModuleExpr(expr)
	default:
		// Anything parsed as other than function call will be treated as a literal string.
		return callExpr_lazyS(strconv.Quote(field))
	}
}

// compileModuleExpr is initially given an `*ast.CallExpr` parsed from the playbook and recursively
// produces an `*ast.CallExpr` from it. When it reaches an `*ast.BasicLit`, it returns a literal string.
func compileModuleExpr(expr ast.Expr) ast.Expr {
	switch expr.(type) {
	case *ast.CallExpr:
		ce := expr.(*ast.CallExpr)
		f := funcName(ce.Fun)
		args := make([]ast.Expr, len(ce.Args))
		for i, arg := range ce.Args {
			args[i] = compileModuleExpr(arg)
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
		return callExpr_lazyS(value)
	default:
		// TODO: Better validation.
		return &ast.CallExpr{Fun: &ast.Ident{Name: invalidIdentifier}}
	}
}

// funcName gets the function name from an `*ast.CallExpr` which has been parsed from
// the playbook and returns a Go qualified identifier for it.
func funcName(expr ast.Expr) string {
	switch expr.(type) {
	case *ast.Ident:
		value := expr.(*ast.Ident).Name
		return fmt.Sprintf(lazyPackageTemplate, util.SnakeToPascal(value))
	default:
		// TODO: Better validation.
		return invalidIdentifier
	}
}
